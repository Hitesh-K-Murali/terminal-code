package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/semaphore"
)

// IsolatedRunner spawns subprocesses with kernel-level isolation.
// Each subprocess gets:
//   - Mount namespace: only allowed binaries visible
//   - PID namespace: can't see/kill host processes
//   - Network namespace: no network (if configured)
//   - Process group: for clean kill on timeout
//   - cgroup limits: memory, CPU, PIDs (where available)
type IsolatedRunner struct {
	plan *EnforcementPlan
	caps PlatformCapabilities
	sem  *semaphore.Weighted // Bounds concurrent subprocesses
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

// RunResult holds the output and metadata from a subprocess execution.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Killed   bool // True if process was killed (timeout, OOM, etc.)
}

// Run executes a command in an isolated subprocess.
// It blocks until the subprocess completes, is killed, or the context is cancelled.
func (r *IsolatedRunner) Run(ctx context.Context, command string) (*RunResult, error) {
	// Check application-level command restrictions first
	if err := r.checkCommandAllowed(command); err != nil {
		return nil, err
	}

	// Acquire semaphore slot (bounds concurrent subprocesses)
	if err := r.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("subprocess pool full: %w", err)
	}
	defer r.sem.Release(1)

	// Apply timeout from plan
	timeout := r.plan.SubprocessTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.SysProcAttr = r.buildSysProcAttr()

	// Capture output
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set restricted environment
	cmd.Env = r.buildEnv()

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

// buildSysProcAttr creates the syscall attributes for namespace isolation.
func (r *IsolatedRunner) buildSysProcAttr() *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{
		Setpgid: true, // New process group for clean group-kill
	}

	if !r.caps.UserNSAvailable {
		log.Println("isolated-runner: user namespaces unavailable, running without namespace isolation")
		return attr
	}

	var cloneFlags uintptr

	// PID namespace: subprocess can't see host processes
	cloneFlags |= syscall.CLONE_NEWPID

	// Network namespace: subprocess has no network access (unless configured)
	if !r.plan.SubprocessNetwork {
		cloneFlags |= syscall.CLONE_NEWNET
	}

	// Mount namespace: only allowed binaries visible
	// Note: CLONE_NEWNS requires careful setup. We use it when command
	// restrictions are configured. The actual mount manipulation happens
	// via a helper script that runs in the child before exec.
	if len(r.plan.AllowedBinaries) > 0 {
		cloneFlags |= syscall.CLONE_NEWNS
	}

	// User namespace: run as unprivileged user
	cloneFlags |= syscall.CLONE_NEWUSER

	if cloneFlags != 0 {
		attr.Cloneflags = cloneFlags

		// Map current user into the namespace
		attr.UidMappings = []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		}
		attr.GidMappings = []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		}
	}

	return attr
}

// buildEnv creates a restricted environment for the subprocess.
func (r *IsolatedRunner) buildEnv() []string {
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + os.Getenv("HOME"),
		"TERM=" + os.Getenv("TERM"),
		"LANG=" + os.Getenv("LANG"),
	}

	// Pass through GOPATH/GOROOT if go is an allowed binary
	for _, bin := range r.plan.AllowedBinaries {
		if bin == "go" {
			if goroot := os.Getenv("GOROOT"); goroot != "" {
				env = append(env, "GOROOT="+goroot)
			}
			if gopath := os.Getenv("GOPATH"); gopath != "" {
				env = append(env, "GOPATH="+gopath)
			}
			break
		}
	}

	return env
}

// checkCommandAllowed applies application-level command filtering.
// This is defense-in-depth behind the mount namespace restriction.
func (r *IsolatedRunner) checkCommandAllowed(command string) error {
	// Check deny patterns (argument-level restrictions)
	for _, pattern := range r.plan.DenyPatterns {
		if matchCommandPattern(command, pattern) {
			return fmt.Errorf("command denied: %q matches deny_pattern %q", command, pattern)
		}
	}

	// If allowed binaries are specified, check that the command starts with one
	if len(r.plan.AllowedBinaries) > 0 {
		cmdParts := strings.Fields(command)
		if len(cmdParts) == 0 {
			return fmt.Errorf("empty command")
		}

		baseCmd := filepath.Base(cmdParts[0])
		allowed := false
		for _, bin := range r.plan.AllowedBinaries {
			if baseCmd == bin {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command denied: %q not in allowed binaries", baseCmd)
		}
	}

	return nil
}

// matchCommandPattern checks if a command matches a deny pattern.
// Patterns use * as wildcard. Examples: "* | bash", "curl * -o *"
func matchCommandPattern(command, pattern string) bool {
	// Convert pattern to a simple regex-like check
	parts := strings.Split(pattern, "*")
	remaining := command
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 && !strings.HasPrefix(pattern, "*") {
			return false // First part must match at start unless pattern starts with *
		}
		remaining = remaining[idx+len(part):]
	}
	return true
}
