package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

type BashTool struct {
	runner   *sandbox.IsolatedRunner
	auditLog *sandbox.AuditLog
	plan     *sandbox.EnforcementPlan
}

func NewBashTool(runner *sandbox.IsolatedRunner, auditLog *sandbox.AuditLog, plan *sandbox.EnforcementPlan) *BashTool {
	return &BashTool{runner: runner, auditLog: auditLog, plan: plan}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Execute a bash command in a sandboxed subprocess. The subprocess runs with restricted permissions: limited binaries, no network (by default), memory/CPU/PID limits enforced by the kernel."
}

func (t *BashTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The bash command to execute"
			}
		},
		"required": ["command"]
	}`)
}

func (t *BashTool) PermissionLevel() PermLevel { return PermExecute }

func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if !t.plan.LLMActions.AllowBashExecution {
		t.auditLog.LogDenied("command",
			fmt.Sprintf("bash execution denied: %s", params.Command),
			sandbox.EnforcementApplication)
		return "", fmt.Errorf("bash execution is disabled in restrictions")
	}

	result, err := t.runner.Run(ctx, params.Command)
	if err != nil {
		t.auditLog.LogDenied("command",
			fmt.Sprintf("bash denied: %s: %v", params.Command, err),
			t.plan.CommandLevel)
		return "", err
	}

	var sb strings.Builder

	if result.Stdout != "" {
		sb.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(result.Stderr)
	}

	if result.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf("\n[exit code: %d]", result.ExitCode))
	}
	if result.Killed {
		sb.WriteString("\n[process killed — timeout or resource limit exceeded]")
	}

	sb.WriteString(fmt.Sprintf("\n[duration: %s]", result.Duration.Round(1e6)))

	return sb.String(), nil
}
