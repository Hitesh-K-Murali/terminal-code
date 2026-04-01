package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hitesh-K-Murali/terminal-code/internal/memory"
)

// DirContextTool gives the LLM fast access to directory summaries
// without reading every file. Backed by the in-memory DirCache.
type DirContextTool struct {
	cache *memory.DirCache
}

func NewDirContextTool(cache *memory.DirCache) *DirContextTool {
	return &DirContextTool{cache: cache}
}

func (t *DirContextTool) Name() string { return "dir_context" }

func (t *DirContextTool) Description() string {
	return `Get a compact summary of a directory's purpose, key files, and structure.
Much faster than reading individual files — use this first to understand a directory,
then use read_file for specific files you need. The summary is cached in memory
for the session, so subsequent calls are instant.`
}

func (t *DirContextTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Directory path to summarize (relative or absolute)"
			},
			"recursive": {
				"type": "boolean",
				"description": "Include subdirectories (default false, max depth 2)"
			}
		},
		"required": ["path"]
	}`)
}

func (t *DirContextTool) PermissionLevel() PermLevel { return PermRead }

func (t *DirContextTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Path == "" {
		params.Path = "."
	}

	summary, err := t.cache.Get(params.Path)
	if err != nil {
		return "", fmt.Errorf("dir_context %s: %w", params.Path, err)
	}

	var sb strings.Builder
	sb.WriteString(summary.ToContext())

	// List all files with sizes
	sb.WriteString("\nFiles:\n")
	for _, f := range summary.Files {
		marker := "  "
		if f.IsEntry {
			marker = "* " // Entry point
		} else if f.IsTest {
			marker = "T " // Test
		} else if f.IsConfig {
			marker = "C " // Config
		}
		sb.WriteString(fmt.Sprintf("  %s%s (%s)\n", marker, f.Name, formatSize(f.Size)))
	}

	if len(summary.Dependencies) > 0 {
		sb.WriteString("\nDependencies: " + strings.Join(summary.Dependencies, ", ") + "\n")
	}

	sb.WriteString(fmt.Sprintf("\n[cached, hash=%s]\n", summary.ContentHash))

	return sb.String(), nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
