package sandbox

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditAction represents what type of security event occurred.
type AuditAction string

const (
	AuditApplied AuditAction = "APPLIED"  // Restriction was applied
	AuditAllowed AuditAction = "ALLOWED"  // Operation was allowed
	AuditDenied  AuditAction = "DENIED"   // Operation was denied
	AuditWarning AuditAction = "WARNING"  // Non-fatal security warning
)

// AuditEntry is a single security audit log entry.
type AuditEntry struct {
	Timestamp   time.Time        `json:"timestamp"`
	Action      AuditAction      `json:"action"`
	Category    string           `json:"category"`    // filesystem, command, network, resource
	Description string           `json:"description"`
	Level       EnforcementLevel `json:"level"`
	Path        string           `json:"path,omitempty"`
	Command     string           `json:"command,omitempty"`
}

// AuditLog records security events for the session.
// Thread-safe — multiple agents can log concurrently.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
	file    *os.File
}

// NewAuditLog creates a new audit log, optionally writing to a file.
func NewAuditLog() *AuditLog {
	al := &AuditLog{}

	// Try to create audit log file
	home, err := os.UserHomeDir()
	if err == nil {
		auditDir := filepath.Join(home, ".tc", "audit")
		os.MkdirAll(auditDir, 0700)

		filename := fmt.Sprintf("session-%s.log", time.Now().Format("2006-01-02T15-04-05"))
		f, err := os.OpenFile(
			filepath.Join(auditDir, filename),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0600,
		)
		if err == nil {
			al.file = f
		}
	}

	return al
}

// Log records a security event.
func (al *AuditLog) Log(entry AuditEntry) {
	entry.Timestamp = time.Now()

	al.mu.Lock()
	al.entries = append(al.entries, entry)
	al.mu.Unlock()

	line := fmt.Sprintf("[%s] %s %s: %s [%s]",
		entry.Timestamp.Format("15:04:05.000"),
		entry.Action,
		entry.Category,
		entry.Description,
		entry.Level)

	if entry.Action == AuditDenied {
		log.Printf("AUDIT %s", line)
	}

	if al.file != nil {
		fmt.Fprintln(al.file, line)
	}
}

// LogDenied is a convenience method for denied operations.
func (al *AuditLog) LogDenied(category, description string, level EnforcementLevel) {
	al.Log(AuditEntry{
		Action:      AuditDenied,
		Category:    category,
		Description: description,
		Level:       level,
	})
}

// LogApplied records a restriction being applied.
func (al *AuditLog) LogApplied(category, description string, level EnforcementLevel) {
	al.Log(AuditEntry{
		Action:      AuditApplied,
		Category:    category,
		Description: description,
		Level:       level,
	})
}

// Entries returns a copy of all audit entries.
func (al *AuditLog) Entries() []AuditEntry {
	al.mu.Lock()
	defer al.mu.Unlock()
	out := make([]AuditEntry, len(al.entries))
	copy(out, al.entries)
	return out
}

// DeniedCount returns the number of denied operations in this session.
func (al *AuditLog) DeniedCount() int {
	al.mu.Lock()
	defer al.mu.Unlock()
	count := 0
	for _, e := range al.entries {
		if e.Action == AuditDenied {
			count++
		}
	}
	return count
}

// Close flushes and closes the audit log file.
func (al *AuditLog) Close() error {
	if al.file != nil {
		return al.file.Close()
	}
	return nil
}
