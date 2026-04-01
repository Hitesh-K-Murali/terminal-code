package sandbox

import (
	"strings"
	"time"
)

// PlatformCapabilities describes what kernel security mechanisms are available
// on this system. All sandbox code checks this before attempting enforcement.
type PlatformCapabilities struct {
	KernelVersion     string
	KernelMajor       int
	KernelMinor       int
	SeccompAvailable  bool
	LandlockAvailable bool
	LandlockABI       int
	CgroupVersion     int // 0=none, 1=v1, 2=v2
	UserNSAvailable   bool
}

// RunResult holds the output and metadata from a subprocess execution.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Killed   bool // True if process was killed (timeout, OOM, etc.)
}

// matchCommandPattern checks if a command matches a deny pattern.
// Patterns use * as wildcard. Examples: "* | bash", "curl * -o *"
func matchCommandPattern(command, pattern string) bool {
	parts := strings.Split(pattern, "*")
	remaining := command
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 && !strings.HasPrefix(pattern, "*") {
			return false
		}
		remaining = remaining[idx+len(part):]
	}
	return true
}
