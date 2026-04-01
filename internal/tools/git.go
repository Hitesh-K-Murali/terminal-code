package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type GitTool struct{}

func NewGitTool() *GitTool { return &GitTool{} }

func (t *GitTool) Name() string { return "git" }

func (t *GitTool) Description() string {
	return `Execute git commands. Supports: status, diff, log, add, commit, branch, checkout, stash.
Does NOT support: push, force operations, or rebase (these require explicit user approval).
All git operations are serialized — only one git command runs at a time.`
}

func (t *GitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "Git subcommand and arguments (e.g., 'status', 'diff --staged', 'log --oneline -10', 'commit -m \"message\"')"
			}
		},
		"required": ["command"]
	}`)
}

func (t *GitTool) PermissionLevel() PermLevel { return PermExecute }

func (t *GitTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Parse the git subcommand
	args := strings.Fields(params.Command)
	if len(args) == 0 {
		return "", fmt.Errorf("empty git command")
	}

	subcommand := args[0]

	// Block dangerous operations
	blocked := map[string]string{
		"push":    "git push requires explicit user approval",
		"rebase":  "git rebase is blocked — too destructive for automated use",
		"reset":   "git reset is blocked — use git checkout or git stash instead",
		"clean":   "git clean is blocked — too destructive",
	}

	if reason, ok := blocked[subcommand]; ok {
		return "", fmt.Errorf("blocked: %s", reason)
	}

	// Block force flags on any command
	for _, arg := range args {
		if arg == "--force" || arg == "-f" {
			return "", fmt.Errorf("blocked: --force flag is not allowed in automated git operations")
		}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Sprintf("%s\n[exit code: %d]", result, exitErr.ExitCode()), nil
		}
		return "", fmt.Errorf("git %s: %w", subcommand, err)
	}

	if result == "" {
		return fmt.Sprintf("git %s: (no output)", subcommand), nil
	}

	return result, nil
}
