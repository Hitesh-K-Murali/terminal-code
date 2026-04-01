//go:build !linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"
)

// IsolatedRunner on non-Linux runs commands without namespace isolation.
type IsolatedRunner struct {
	plan *EnforcementPlan
	caps PlatformCapabilities
	sem  *semaphore.Weighted
}

func NewIsolatedRunner(plan *EnforcementPlan, caps PlatformCapabilities) *IsolatedRunner {
	maxConcurrent := plan.MaxConcurrentSubs
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &IsolatedRunner{
		plan: plan,
		caps: caps,
		sem:  semaphore.NewWeighted(int64(maxConcurrent)),
	}
}

func (r *IsolatedRunner) Run(ctx context.Context, command string) (*RunResult, error) {
	if err := r.checkCommandAllowed(command); err != nil {
		return nil, err
	}

	if err := r.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("subprocess pool full: %w", err)
	}
	defer r.sem.Release(1)

	timeout := r.plan.SubprocessTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &RunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			if ctx.Err() != nil {
				result.Killed = true
			}
		} else {
			return result, fmt.Errorf("exec: %w", err)
		}
	}

	return result, nil
}

func (r *IsolatedRunner) checkCommandAllowed(command string) error {
	for _, pattern := range r.plan.DenyPatterns {
		if matchCommandPattern(command, pattern) {
			return fmt.Errorf("command denied: %q matches deny_pattern %q", command, pattern)
		}
	}
	return nil
}
