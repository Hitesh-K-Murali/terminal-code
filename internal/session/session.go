package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
)

// Session represents a saved conversation with metadata.
type Session struct {
	ID        string             `json:"id"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	Messages  []provider.Message `json:"messages"`
	Stats     SessionStats       `json:"stats"`
	WorkDir   string             `json:"work_dir"`
}

// SessionStats tracks session-level metrics.
type SessionStats struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	ToolCalls    int     `json:"tool_calls"`
	Duration     string  `json:"duration"`
}

// Manager handles session persistence.
type Manager struct {
	dir     string
	current *Session
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".tc", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	return &Manager{dir: dir}, nil
}

// New creates a new session.
func (m *Manager) New(providerName, model string) *Session {
	wd, _ := os.Getwd()
	s := &Session{
		ID:        generateID(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Provider:  providerName,
		Model:     model,
		WorkDir:   wd,
	}
	m.current = s
	return s
}

// Current returns the active session.
func (m *Manager) Current() *Session {
	return m.current
}

// Save persists the current session to disk.
func (m *Manager) Save() error {
	if m.current == nil {
		return nil
	}

	m.current.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(m.current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := filepath.Join(m.dir, m.current.ID+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}

	return nil
}

// Load restores a session from disk.
func (m *Manager) Load(id string) (*Session, error) {
	path := filepath.Join(m.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session %s: %w", id, err)
	}

	m.current = &s
	return &s, nil
}

// List returns all saved sessions, newest first.
func (m *Manager) List() ([]SessionSummary, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}

	var summaries []SessionSummary
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue
		}

		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}

		msgCount := len(s.Messages)
		preview := ""
		if msgCount > 0 {
			preview = s.Messages[0].Content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
		}

		summaries = append(summaries, SessionSummary{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			Model:     s.Model,
			Messages:  msgCount,
			Preview:   preview,
			CostUSD:   s.Stats.TotalCostUSD,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// Delete removes a saved session.
func (m *Manager) Delete(id string) error {
	return os.Remove(filepath.Join(m.dir, id+".json"))
}

// SessionSummary is a lightweight view of a session for listing.
type SessionSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Model     string    `json:"model"`
	Messages  int       `json:"messages"`
	Preview   string    `json:"preview"`
	CostUSD   float64   `json:"cost_usd"`
}

func generateID() string {
	return time.Now().Format("20060102-150405")
}
