package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

var (
	dPass = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
	dWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
	dFail = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
	dInfo = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
)

func pass(msg string) { fmt.Printf("  %s %s\n", dPass.Render("✓"), msg) }
func warn(msg string) { fmt.Printf("  %s %s\n", dWarn.Render("!"), msg) }
func fail(msg string) { fmt.Printf("  %s %s\n", dFail.Render("✗"), msg) }

// RunDoctor runs diagnostic checks and prints a report.
func RunDoctor(version string) error {
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render("  tc doctor"))
	fmt.Println()

	passed, warned, failed := 0, 0, 0

	// 1. Version
	fmt.Printf("  %s Version: %s\n", dInfo.Render("i"), version)
	fmt.Println()

	// 2. Config file
	if ConfigExists() {
		pass(fmt.Sprintf("Config file: %s", ConfigPath()))
		passed++

		cfg, err := LoadConfig()
		if err != nil {
			if errors.Is(err, ErrNoConfig) {
				fail("Config exists but has no API key — run: tc setup")
				failed++
			} else {
				fail(fmt.Sprintf("Config parse error: %v", err))
				failed++
			}
		} else {
			pass(fmt.Sprintf("Provider: %s | Model: %s", FormatProvider(cfg.Provider), cfg.Model))
			passed++

			// Check config file permissions
			info, _ := os.Stat(ConfigPath())
			if info != nil {
				perm := info.Mode().Perm()
				if perm&0077 != 0 {
					warn(fmt.Sprintf("Config file permissions %o — should be 0600 (contains API key)", perm))
					warned++
				} else {
					pass("Config file permissions: secure (0600)")
					passed++
				}
			}

			// Validate config
			issues := ValidateConfig(cfg)
			if len(issues) > 0 {
				for _, issue := range issues {
					warn(fmt.Sprintf("Config: %s", issue))
					warned++
				}
			}

			// 3. API connectivity
			if cfg.Provider != "ollama" {
				checkAPIConnectivity(cfg)
				passed++ // Count the attempt
			} else {
				checkOllamaConnectivity(cfg)
				passed++
			}
		}
	} else {
		fail("No config file — run: tc setup")
		failed++
	}

	fmt.Println()

	// 4. Kernel features
	caps := sandbox.DetectPlatform()

	if caps.SeccompAvailable {
		pass("seccomp-bpf: available")
		passed++
	} else {
		warn("seccomp-bpf: unavailable — process-wide syscall filtering disabled")
		warned++
	}

	if caps.LandlockAvailable {
		pass(fmt.Sprintf("Landlock: available (ABI v%d) — kernel filesystem enforcement active", caps.LandlockABI))
		passed++
	} else {
		warn(fmt.Sprintf("Landlock: unavailable (kernel %s < 5.13) — filesystem restrictions are application-level only", caps.KernelVersion))
		warned++
	}

	switch caps.CgroupVersion {
	case 2:
		pass("cgroups: v2 (unified) — full resource limits available")
		passed++
	case 1:
		pass("cgroups: v1 — resource limits available (legacy API)")
		passed++
	default:
		warn("cgroups: unavailable — subprocess resource limits disabled")
		warned++
	}

	if caps.UserNSAvailable {
		pass("User namespaces: available — subprocess isolation active")
		passed++
	} else {
		warn("User namespaces: unavailable — subprocess isolation disabled")
		warned++
	}

	fmt.Println()

	// 5. Project restrictions
	wd, _ := os.Getwd()
	restrictPath := wd + "/.tc/restrictions.toml"
	if _, err := os.Stat(restrictPath); err == nil {
		restrictions, err := sandbox.LoadRestrictions()
		if err != nil {
			fail(fmt.Sprintf("Project restrictions error: %v", err))
			failed++
		} else {
			warnings := restrictions.Validate()
			if len(warnings) == 0 {
				pass("Project restrictions: valid")
				passed++
			} else {
				for _, w := range warnings {
					warn(fmt.Sprintf("Restriction: %s", w))
					warned++
				}
			}
		}
	} else {
		fmt.Printf("  %s No project restrictions (run: tc init)\n", dInfo.Render("i"))
	}

	// Summary
	fmt.Println()
	fmt.Printf("  %d passed", passed)
	if warned > 0 {
		fmt.Printf(" | %d warnings", warned)
	}
	if failed > 0 {
		fmt.Printf(" | %d failed", failed)
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}
	return nil
}

func checkAPIConnectivity(cfg *Config) {
	host := "api.anthropic.com:443"
	if cfg.Provider == "openai" {
		host = "api.openai.com:443"
	}
	if cfg.BaseURL != "" {
		// Extract host from base URL
		h := cfg.BaseURL
		for _, prefix := range []string{"https://", "http://"} {
			h = trimPrefix(h, prefix)
		}
		if idx := indexByte(h, '/'); idx > 0 {
			h = h[:idx]
		}
		if !containsByte(h, ':') {
			h += ":443"
		}
		host = h
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
	elapsed := time.Since(start)

	if err != nil {
		fail(fmt.Sprintf("Cannot reach %s: %v", host, err))
	} else {
		conn.Close()
		pass(fmt.Sprintf("API reachable: %s (%s)", host, elapsed.Round(time.Millisecond)))
	}
}

func checkOllamaConnectivity(cfg *Config) {
	host := "localhost:11434"
	if cfg.OllamaURL != "" {
		h := cfg.OllamaURL
		for _, prefix := range []string{"https://", "http://"} {
			h = trimPrefix(h, prefix)
		}
		if idx := indexByte(h, '/'); idx > 0 {
			h = h[:idx]
		}
		host = h
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
	if err != nil {
		warn(fmt.Sprintf("Ollama not reachable at %s — is it running?", host))
	} else {
		conn.Close()
		pass(fmt.Sprintf("Ollama reachable: %s", host))
	}
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func containsByte(s string, b byte) bool {
	return indexByte(s, b) >= 0
}
