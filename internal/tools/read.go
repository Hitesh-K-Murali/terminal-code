package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

type ReadFileTool struct {
	checker  *sandbox.PathChecker
	auditLog *sandbox.AuditLog
}

func NewReadFileTool(checker *sandbox.PathChecker, auditLog *sandbox.AuditLog) *ReadFileTool {
	return &ReadFileTool{checker: checker, auditLog: auditLog}
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file. Returns the file content as a string with line numbers."
}

func (t *ReadFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative path to the file to read"
			},
			"offset": {
				"type": "integer",
				"description": "Line number to start reading from (0-based)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of lines to read"
			}
		},
		"required": ["path"]
	}`)
}

func (t *ReadFileTool) PermissionLevel() PermLevel { return PermRead }

func (t *ReadFileTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Check filesystem restrictions
	if err := t.checker.CheckRead(params.Path); err != nil {
		t.auditLog.LogDenied("filesystem", fmt.Sprintf("read_file %s: %v", params.Path, err), sandbox.EnforcementApplication)
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", params.Path, err)
	}

	lines := strings.Split(string(data), "\n")

	// Apply offset and limit
	if params.Offset > 0 && params.Offset < len(lines) {
		lines = lines[params.Offset:]
	}
	if params.Limit > 0 && params.Limit < len(lines) {
		lines = lines[:params.Limit]
	}

	// Format with line numbers
	var sb strings.Builder
	startLine := params.Offset + 1
	for i, line := range lines {
		fmt.Fprintf(&sb, "%d\t%s\n", startLine+i, line)
	}

	return sb.String(), nil
}
