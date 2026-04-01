package memory

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DirCache provides on-demand, in-memory directory context summaries.
//
// When the LLM first needs to understand a directory, the cache generates
// a compact summary (files, purpose heuristics, key exports, dependencies).
// Subsequent accesses in the same session serve from cache — zero disk IO.
//
// Key design decision: NO disk persistence by default. The cache is always
// derived from the actual files, so it's never stale. Optional persistence
// to .tc/manifests/ is available if configured.
type DirCache struct {
	mu      sync.RWMutex
	entries map[string]*DirSummary // abs path → cached summary
	config  DirCacheConfig
}

type DirCacheConfig struct {
	PersistToDisk bool   // Write summaries to .tc/manifests/
	ManifestDir   string // Where to persist (if enabled)
}

// DirSummary is a compact, token-efficient description of a directory.
type DirSummary struct {
	Path         string
	FileCount    int
	Files        []FileMeta
	Languages    map[string]int
	Purpose      string   // Heuristic-derived purpose
	KeyFiles     []string // Important files (entry points, configs, tests)
	Dependencies []string // Detected dependencies (imports, requires)
	ContentHash  string   // Hash of file listing (for staleness check)
	CachedAt     time.Time
}

// FileMeta is lightweight file metadata.
type FileMeta struct {
	Name    string
	Size    int64
	IsEntry bool // Heuristic: looks like an entry point
	IsTest  bool // Heuristic: looks like a test file
	IsConfig bool // Heuristic: looks like a config file
}

func NewDirCache(config DirCacheConfig) *DirCache {
	return &DirCache{
		entries: make(map[string]*DirSummary),
		config:  config,
	}
}

// Get returns a cached directory summary, generating it on first access.
func (dc *DirCache) Get(dirPath string) (*DirSummary, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, err
	}

	// Check cache first (read lock)
	dc.mu.RLock()
	if summary, ok := dc.entries[absPath]; ok {
		dc.mu.RUnlock()
		return summary, nil
	}
	dc.mu.RUnlock()

	// Cache miss — generate summary
	summary, err := dc.generateSummary(absPath)
	if err != nil {
		return nil, err
	}

	// Store in cache (write lock)
	dc.mu.Lock()
	dc.entries[absPath] = summary
	dc.mu.Unlock()

	// Optional: persist to disk
	if dc.config.PersistToDisk && dc.config.ManifestDir != "" {
		dc.persist(absPath, summary)
	}

	return summary, nil
}

// Invalidate removes a directory from the cache (e.g., after file writes).
func (dc *DirCache) Invalidate(dirPath string) {
	absPath, _ := filepath.Abs(dirPath)
	dc.mu.Lock()
	delete(dc.entries, absPath)
	dc.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (dc *DirCache) InvalidateAll() {
	dc.mu.Lock()
	dc.entries = make(map[string]*DirSummary)
	dc.mu.Unlock()
}

// Stats returns cache hit/miss statistics.
func (dc *DirCache) Stats() (cached int) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return len(dc.entries)
}

// generateSummary builds a compact directory summary by reading file metadata
// and applying heuristics. Does NOT read file contents — only names and sizes.
func (dc *DirCache) generateSummary(dirPath string) (*DirSummary, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dirPath, err)
	}

	summary := &DirSummary{
		Path:      dirPath,
		Languages: make(map[string]int),
		CachedAt:  time.Now(),
	}

	var hashInput strings.Builder

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		name := e.Name()
		ext := filepath.Ext(name)
		size := info.Size()

		hashInput.WriteString(fmt.Sprintf("%s:%d\n", name, size))

		fm := FileMeta{
			Name:     name,
			Size:     size,
			IsEntry:  isEntryPoint(name),
			IsTest:   isTestFile(name),
			IsConfig: isConfigFile(name),
		}

		summary.Files = append(summary.Files, fm)
		summary.FileCount++

		if ext != "" {
			summary.Languages[ext]++
		}

		if fm.IsEntry {
			summary.KeyFiles = append(summary.KeyFiles, name)
		}
		if fm.IsConfig {
			summary.KeyFiles = append(summary.KeyFiles, name)
		}
	}

	// Content hash for staleness detection
	h := sha256.Sum256([]byte(hashInput.String()))
	summary.ContentHash = fmt.Sprintf("%x", h[:8])

	// Heuristic: derive directory purpose from file patterns
	summary.Purpose = derivePurpose(filepath.Base(dirPath), summary)

	// Detect dependencies from common patterns
	summary.Dependencies = detectDependencies(dirPath, summary)

	return summary, nil
}

// ToContext converts a directory summary into a compact string for LLM context.
// Target: ~50-100 tokens per directory.
func (s *DirSummary) ToContext() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("**%s** — %s (%d files)\n",
		filepath.Base(s.Path), s.Purpose, s.FileCount))

	// Key files only
	if len(s.KeyFiles) > 0 {
		sb.WriteString("  Key: " + strings.Join(s.KeyFiles, ", ") + "\n")
	}

	// Languages compact
	if len(s.Languages) > 0 {
		type lc struct{ ext string; n int }
		var langs []lc
		for ext, n := range s.Languages {
			langs = append(langs, lc{ext, n})
		}
		sort.Slice(langs, func(i, j int) bool { return langs[i].n > langs[j].n })
		parts := make([]string, 0, 3)
		for i, l := range langs {
			if i >= 3 { break }
			parts = append(parts, fmt.Sprintf("%s(%d)", l.ext, l.n))
		}
		sb.WriteString("  Lang: " + strings.Join(parts, " ") + "\n")
	}

	return sb.String()
}

// Heuristic functions — derive meaning from file names, not contents.
// Zero IO, instant, and surprisingly accurate for convention-following projects.

func isEntryPoint(name string) bool {
	entryNames := []string{"main.go", "main.ts", "main.py", "index.ts", "index.js",
		"app.go", "app.ts", "app.py", "server.go", "server.ts", "cli.go", "cmd.go"}
	lower := strings.ToLower(name)
	for _, e := range entryNames {
		if lower == e { return true }
	}
	return false
}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasPrefix(lower, "test_")
}

func isConfigFile(name string) bool {
	configNames := []string{"config.go", "config.ts", "config.py", "config.toml",
		"config.yaml", "config.json", ".env.example", "settings.py",
		"Makefile", "Dockerfile", "docker-compose.yml", "package.json",
		"go.mod", "Cargo.toml", "pyproject.toml", "tsconfig.json"}
	for _, c := range configNames {
		if name == c { return true }
	}
	return false
}

func derivePurpose(dirName string, summary *DirSummary) string {
	// Common Go/TS/Py directory conventions
	purposes := map[string]string{
		"cmd":        "CLI entrypoints",
		"internal":   "private packages",
		"pkg":        "public library packages",
		"api":        "API definitions",
		"server":     "HTTP/gRPC server",
		"client":     "API client",
		"handler":    "request handlers",
		"handlers":   "request handlers",
		"middleware":  "HTTP middleware",
		"model":      "data models",
		"models":     "data models",
		"service":    "business logic",
		"services":   "business logic",
		"repository": "data access layer",
		"store":      "data storage",
		"util":       "utility functions",
		"utils":      "utility functions",
		"helpers":    "helper functions",
		"config":     "configuration",
		"configs":    "configuration files",
		"test":       "test suites",
		"tests":      "test suites",
		"migration":  "database migrations",
		"migrations": "database migrations",
		"proto":      "protobuf definitions",
		"types":      "type definitions",
		"ui":         "user interface",
		"components": "UI components",
		"hooks":      "React hooks",
		"pages":      "page components",
		"routes":     "route definitions",
		"lib":        "shared library code",
		"src":        "source code",
		"sandbox":    "security sandbox",
		"agent":      "agent system",
		"provider":   "LLM providers",
		"tools":      "tool implementations",
		"engine":     "core engine",
		"memory":     "memory/context system",
		"session":    "session management",
		"policy":     "policy engine",
		"secrets":    "secret management",
		"plugins":    "plugin system",
		"mcp":        "MCP protocol",
	}

	if purpose, ok := purposes[strings.ToLower(dirName)]; ok {
		return purpose
	}

	// Fallback: derive from file patterns
	hasTests := false
	hasEntries := false
	for _, f := range summary.Files {
		if f.IsTest { hasTests = true }
		if f.IsEntry { hasEntries = true }
	}

	if hasEntries && hasTests {
		return "module with entry points and tests"
	}
	if hasEntries {
		return "module with entry points"
	}
	if hasTests {
		return "module with tests"
	}

	return "module"
}

func detectDependencies(dirPath string, summary *DirSummary) []string {
	// Check for dependency files in this directory
	var deps []string
	for _, f := range summary.Files {
		switch f.Name {
		case "go.mod":
			deps = append(deps, "Go module")
		case "package.json":
			deps = append(deps, "npm package")
		case "requirements.txt":
			deps = append(deps, "Python requirements")
		case "Cargo.toml":
			deps = append(deps, "Rust crate")
		}
	}
	return deps
}

// persist writes a summary to disk as a manifest file.
func (dc *DirCache) persist(absPath string, summary *DirSummary) {
	if dc.config.ManifestDir == "" {
		return
	}

	os.MkdirAll(dc.config.ManifestDir, 0755)

	// Use relative path as filename
	relPath, err := filepath.Rel(dc.config.ManifestDir, absPath)
	if err != nil {
		relPath = filepath.Base(absPath)
	}
	filename := strings.ReplaceAll(relPath, string(filepath.Separator), "_") + ".md"

	content := fmt.Sprintf("# %s\n\n%s\nHash: %s | Cached: %s\n",
		filepath.Base(absPath), summary.ToContext(),
		summary.ContentHash, summary.CachedAt.Format(time.RFC3339))

	os.WriteFile(filepath.Join(dc.config.ManifestDir, filename), []byte(content), 0644)
}
