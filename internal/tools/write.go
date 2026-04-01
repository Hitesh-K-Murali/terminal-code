package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

type WriteFileTool struct {
	checker  *sandbox.PathChecker
	auditLog *sandbox.AuditLog
	plan     *sandbox.EnforcementPlan
}

func NewWriteFileTool(checker *sandbox.PathChecker, auditLog *sandbox.AuditLog, plan *sandbox.EnforcementPlan) *WriteFileTool {
	return &WriteFileTool{checker: checker, auditLog: auditLog, plan: plan}
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Creates parent directories as needed."
}

func (t *WriteFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative path to the file to write"
			},
			"content": {
				"type": "string",
				"description": "The content to write to the file"
			}
		},
		"required": ["path", "content"]
	}`)
}

func (t *WriteFileTool) PermissionLevel() PermLevel { return PermWrite }

func (t *WriteFileTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Check filesystem restrictions
	if err := t.checker.CheckWrite(params.Path); err != nil {
		t.auditLog.LogDenied("filesystem",
			fmt.Sprintf("write_file %s: %v", params.Path, err),
			t.plan.FilesystemLevel)
		return "", err
	}

	// Check file size limit
	if t.plan.MaxFileSizeWriteBytes > 0 && int64(len(params.Content)) > t.plan.MaxFileSizeWriteBytes {
		return "", fmt.Errorf("file content exceeds max_file_size_write (%d bytes)", t.plan.MaxFileSizeWriteBytes)
	}

	// Create parent directories
	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", params.Path, err)
	}

	return fmt.Sprintf("Written %d bytes to %s", len(params.Content), params.Path), nil
}
