package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GrepTool struct{}

func NewGrepTool() *GrepTool { return &GrepTool{} }

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return "Search file contents for a regex pattern. Returns matching lines with file paths and line numbers."
}

func (t *GrepTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Regular expression pattern to search for"
			},
			"path": {
				"type": "string",
				"description": "File or directory to search in (defaults to current directory)"
			},
			"glob": {
				"type": "string",
				"description": "File glob filter (e.g., '*.go', '*.ts')"
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum number of results to return (default 50)"
			}
		},
		"required": ["pattern"]
	}`)
}

func (t *GrepTool) PermissionLevel() PermLevel { return PermRead }

func (t *GrepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Path == "" {
		params.Path = "."
	}
	if params.MaxResults <= 0 {
		params.MaxResults = 50
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", params.Pattern, err)
	}

	var results []string
	count := 0

	err = filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if count >= params.MaxResults {
			return filepath.SkipAll
		}

		// Apply glob filter
		if params.Glob != "" {
			matched, _ := filepath.Match(params.Glob, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Skip binary files
		if isBinaryFile(path) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil // Skip unreadable files
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if count >= params.MaxResults {
				break
			}
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", path, lineNum, line))
				count++
			}
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("search error: %w", err)
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}

	return fmt.Sprintf("%d matches:\n%s", len(results), strings.Join(results, "\n")), nil
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return true
	}

	// Check for null bytes (binary indicator)
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
