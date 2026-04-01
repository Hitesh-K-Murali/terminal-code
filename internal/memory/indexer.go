package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectIndex is a compact, token-efficient snapshot of the project.
// Instead of dumping file contents (which wastes tokens), it provides:
//   - File tree (structure at a glance)
//   - Key file summaries (1-2 lines each, not full content)
//   - Language breakdown
//   - Pointers: "use read_file to see the full content"
//
// This gives the LLM enough awareness to make smart tool calls
// without consuming thousands of tokens on every request.
type ProjectIndex struct {
	RootDir    string
	FileTree   string
	KeyFiles   map[string]string // Filename → 1-2 line summary (NOT full content)
	Languages  map[string]int
	TotalFiles int
	IndexedAt  time.Time
}

// IndexProject scans the project directory and builds a compact context index.
// Designed to use minimal tokens while giving the LLM full project awareness.
func IndexProject(rootDir string) (*ProjectIndex, error) {
	idx := &ProjectIndex{
		RootDir:   rootDir,
		KeyFiles:  make(map[string]string),
		Languages: make(map[string]int),
		IndexedAt: time.Now(),
	}

	// Key files: read only the FIRST FEW LINES for a summary
	priorityFiles := []string{
		"README.md", "go.mod", "package.json", "Cargo.toml",
		"pyproject.toml", "Makefile", "Dockerfile", "CLAUDE.md",
	}

	for _, name := range priorityFiles {
		path := filepath.Join(rootDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Only keep first 3 lines as a summary — NOT the full file
		lines := strings.SplitN(string(data), "\n", 4)
		summary := strings.Join(lines[:min(3, len(lines))], "\n")
		idx.KeyFiles[name] = strings.TrimSpace(summary)
	}

	// Build compact file tree
	var treeEntries []treeEntry
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(rootDir, path)
		if rel == "." {
			return nil
		}

		base := filepath.Base(path)

		// Skip noise directories
		if info.IsDir() {
			skipDirs := []string{".git", ".claude", "node_modules", "vendor",
				"dist", "build", "__pycache__", ".next", "target", ".tc"}
			for _, skip := range skipDirs {
				if base == skip {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip binaries and large files
		if info.Size() > 512*1024 { // >512KB
			return nil
		}
		skipExts := []string{".exe", ".bin", ".o", ".so", ".dylib",
			".png", ".jpg", ".gif", ".ico", ".svg", ".pdf", ".zip", ".tar", ".wasm"}
		ext := filepath.Ext(base)
		for _, skip := range skipExts {
			if ext == skip {
				return nil
			}
		}

		idx.TotalFiles++
		if ext != "" {
			idx.Languages[ext]++
		}

		treeEntries = append(treeEntries, treeEntry{
			path:    rel,
			size:    info.Size(),
			modTime: info.ModTime(),
		})

		return nil
	})
	if err != nil {
		return idx, err
	}

	idx.FileTree = buildCompactTree(treeEntries)
	return idx, nil
}

type treeEntry struct {
	path    string
	size    int64
	modTime time.Time
}

// buildCompactTree creates a compact file tree representation.
// Groups by directory, shows file counts for large dirs instead of listing every file.
func buildCompactTree(entries []treeEntry) string {
	if len(entries) == 0 {
		return "(empty project)"
	}

	// Group by top-level directory
	dirFiles := make(map[string][]string)
	var rootFiles []string

	for _, e := range entries {
		parts := strings.SplitN(e.path, string(filepath.Separator), 2)
		if len(parts) == 1 {
			rootFiles = append(rootFiles, e.path)
		} else {
			dirFiles[parts[0]] = append(dirFiles[parts[0]], parts[1])
		}
	}

	var sb strings.Builder

	// Root files first
	for _, f := range rootFiles {
		sb.WriteString(f + "\n")
	}

	// Then directories
	dirs := make([]string, 0, len(dirFiles))
	for d := range dirFiles {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		files := dirFiles[dir]
		if len(files) <= 8 {
			// Show all files
			sb.WriteString(dir + "/\n")
			for _, f := range files {
				sb.WriteString("  " + f + "\n")
			}
		} else {
			// Summarize: show first 4 files + count
			sb.WriteString(fmt.Sprintf("%s/ (%d files)\n", dir, len(files)))
			for _, f := range files[:4] {
				sb.WriteString("  " + f + "\n")
			}
			sb.WriteString(fmt.Sprintf("  ... +%d more\n", len(files)-4))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// ToSystemPrompt converts the index into a token-efficient system prompt section.
// Key design: references instead of content. The LLM uses read_file for details.
func (idx *ProjectIndex) ToSystemPrompt() string {
	var sb strings.Builder

	sb.WriteString("## Project Context\n\n")
	sb.WriteString(fmt.Sprintf("Root: %s | %d files | Indexed: %s\n\n",
		idx.RootDir, idx.TotalFiles, idx.IndexedAt.Format("15:04:05")))

	// Languages (compact)
	if len(idx.Languages) > 0 {
		type lc struct {
			ext   string
			count int
		}
		var langs []lc
		for ext, count := range idx.Languages {
			langs = append(langs, lc{ext, count})
		}
		sort.Slice(langs, func(i, j int) bool { return langs[i].count > langs[j].count })
		parts := make([]string, 0, 5)
		for i, l := range langs {
			if i >= 5 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s(%d)", l.ext, l.count))
		}
		sb.WriteString("Languages: " + strings.Join(parts, " ") + "\n\n")
	}

	// File tree (compact)
	sb.WriteString("```\n" + idx.FileTree + "\n```\n\n")

	// Key file summaries (NOT full content — just pointers)
	if len(idx.KeyFiles) > 0 {
		sb.WriteString("Key files (use `read_file` for full content):\n")
		for name, summary := range idx.KeyFiles {
			// Single-line summary
			firstLine := strings.SplitN(summary, "\n", 2)[0]
			if len(firstLine) > 80 {
				firstLine = firstLine[:80] + "..."
			}
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", name, firstLine))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ContextBuilder combines project index + memory store into a single
// system prompt addition. Designed to be token-efficient.
type ContextBuilder struct {
	index  *ProjectIndex
	memory *Store
}

func NewContextBuilder(index *ProjectIndex, memory *Store) *ContextBuilder {
	return &ContextBuilder{index: index, memory: memory}
}

// Build returns the combined context string.
// Total target: <500 tokens for a typical project.
func (cb *ContextBuilder) Build() string {
	var sb strings.Builder

	if cb.index != nil {
		sb.WriteString(cb.index.ToSystemPrompt())
	}

	if cb.memory != nil {
		entries, _ := cb.memory.List()
		if len(entries) > 0 {
			sb.WriteString("## Project Memory\n\n")
			for _, e := range entries {
				// Only include the first line of each memory entry
				firstLine := strings.SplitN(e.Content, "\n", 2)[0]
				if len(firstLine) > 100 {
					firstLine = firstLine[:100] + "..."
				}
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", e.Name, firstLine))
			}
			sb.WriteString("\nUse `read_file .tc/memory/<name>.md` for full memory content.\n")
		}
	}

	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
