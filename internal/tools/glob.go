package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type GlobTool struct{}

func NewGlobTool() *GlobTool { return &GlobTool{} }

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern. Returns matching file paths sorted by modification time (newest first)."
}

func (t *GlobTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Glob pattern to match files (e.g., '**/*.go', 'src/**/*.ts')"
			},
			"path": {
				"type": "string",
				"description": "Base directory to search in (defaults to current directory)"
			}
		},
		"required": ["pattern"]
	}`)
}

func (t *GlobTool) PermissionLevel() PermLevel { return PermRead }

func (t *GlobTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	base := params.Path
	if base == "" {
		base = "."
	}

	var matches []string

	// Handle ** recursive glob
	if strings.Contains(params.Pattern, "**") {
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			// Match the filename part against the non-** portion
			rel, _ := filepath.Rel(base, path)
			if matchDoubleGlob(rel, params.Pattern) {
				matches = append(matches, path)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk %s: %w", base, err)
		}
	} else {
		pattern := filepath.Join(base, params.Pattern)
		var err error
		matches, err = filepath.Glob(pattern)
		if err != nil {
			return "", fmt.Errorf("glob %s: %w", pattern, err)
		}
	}

	// Sort by modification time (newest first)
	type fileEntry struct {
		path    string
		modTime int64
	}
	entries := make([]fileEntry, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		entries = append(entries, fileEntry{path: m, modTime: info.ModTime().Unix()})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime > entries[j].modTime
	})

	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.path)
		sb.WriteByte('\n')
	}

	if len(entries) == 0 {
		return "No files matched the pattern.", nil
	}

	return fmt.Sprintf("%d files matched:\n%s", len(entries), sb.String()), nil
}

// matchDoubleGlob handles ** patterns by splitting on ** and matching segments.
func matchDoubleGlob(path, pattern string) bool {
	// Simple approach: extract the file extension/name pattern after **
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return false
	}

	suffix := strings.TrimPrefix(parts[1], "/")
	if suffix == "" {
		return true
	}

	// Match against the suffix pattern
	matched, _ := filepath.Match(suffix, filepath.Base(path))
	return matched
}
