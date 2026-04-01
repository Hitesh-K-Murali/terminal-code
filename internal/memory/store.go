package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store manages persistent project-level memory across sessions.
// Memory files live in .tc/memory/ within the project directory.
type Store struct {
	dir string
}

// Entry is a single memory record.
type Entry struct {
	Name        string
	Description string
	Content     string
	ModTime     time.Time
}

// NewStore creates a memory store in the given directory.
func NewStore(projectDir string) (*Store, error) {
	dir := filepath.Join(projectDir, ".tc", "memory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save writes a memory entry to disk.
func (s *Store) Save(name, content string) error {
	filename := sanitizeFilename(name) + ".md"
	path := filepath.Join(s.dir, filename)
	return os.WriteFile(path, []byte(content), 0644)
}

// Load reads a specific memory entry.
func (s *Store) Load(name string) (*Entry, error) {
	filename := sanitizeFilename(name) + ".md"
	path := filepath.Join(s.dir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	info, _ := os.Stat(path)
	modTime := time.Now()
	if info != nil {
		modTime = info.ModTime()
	}

	return &Entry{
		Name:    name,
		Content: string(data),
		ModTime: modTime,
	}, nil
}

// List returns all memory entries.
func (s *Store) List() ([]Entry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var memories []Entry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}

		info, _ := e.Info()
		modTime := time.Now()
		if info != nil {
			modTime = info.ModTime()
		}

		name := strings.TrimSuffix(e.Name(), ".md")
		memories = append(memories, Entry{
			Name:    name,
			Content: string(data),
			ModTime: modTime,
		})
	}

	return memories, nil
}

// Delete removes a memory entry.
func (s *Store) Delete(name string) error {
	filename := sanitizeFilename(name) + ".md"
	return os.Remove(filepath.Join(s.dir, filename))
}

// LoadAll returns all memory content as a single string for context injection.
func (s *Store) LoadAll() string {
	entries, err := s.List()
	if err != nil || len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Memory\n\n")
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", e.Name, e.Content))
	}
	return sb.String()
}

func sanitizeFilename(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, name)
	if name == "" {
		name = "unnamed"
	}
	return name
}
