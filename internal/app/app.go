package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Hitesh-K-Murali/terminal-code/internal/engine"
	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
	"github.com/Hitesh-K-Murali/terminal-code/internal/ui"
)

func Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1. Detect platform capabilities
	caps := sandbox.DetectPlatform()
	reportCapabilities(caps)

	// 2. Apply process-wide security (seccomp)
	if err := sandbox.ApplyProcessSecurity(caps); err != nil {
		log.Printf("warning: failed to apply process security: %v", err)
	}

	// 3. Load config
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 4. Initialize provider
	p, err := provider.New(cfg.Provider, cfg.APIKey, cfg.Model)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}

	// 5. Create engine
	eng := engine.New(p)

	// 6. Start TUI
	model := ui.NewModel(eng, cfg.Model)
	program := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	return nil
}

func reportCapabilities(caps sandbox.PlatformCapabilities) {
	fmt.Fprintf(os.Stderr, "Platform: kernel %s\n", caps.KernelVersion)

	if caps.SeccompAvailable {
		fmt.Fprintln(os.Stderr, "  seccomp-bpf: available")
	} else {
		fmt.Fprintln(os.Stderr, "  seccomp-bpf: unavailable")
	}

	if caps.LandlockAvailable {
		fmt.Fprintf(os.Stderr, "  landlock: available (ABI v%d)\n", caps.LandlockABI)
	} else {
		fmt.Fprintln(os.Stderr, "  landlock: unavailable (kernel < 5.13)")
	}

	switch caps.CgroupVersion {
	case 2:
		fmt.Fprintln(os.Stderr, "  cgroups: v2 (unified)")
	case 1:
		fmt.Fprintln(os.Stderr, "  cgroups: v1 (legacy)")
	default:
		fmt.Fprintln(os.Stderr, "  cgroups: unavailable")
	}

	if caps.UserNSAvailable {
		fmt.Fprintln(os.Stderr, "  user namespaces: available")
	} else {
		fmt.Fprintln(os.Stderr, "  user namespaces: unavailable")
	}

	fmt.Fprintln(os.Stderr)
}
