package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Hitesh-K-Murali/terminal-code/internal/engine"
	"github.com/Hitesh-K-Murali/terminal-code/internal/memory"
	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
	"github.com/Hitesh-K-Murali/terminal-code/internal/tools"
	"github.com/Hitesh-K-Murali/terminal-code/internal/ui"
)

func Run(ctx context.Context) error {
	// Suppress default log timestamps — we handle our own output
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	printBanner()

	// 1. Detect platform capabilities
	caps := sandbox.DetectPlatform()
	printCapabilities(caps)

	// 2. Apply process-wide security (seccomp)
	if err := sandbox.ApplyProcessSecurity(caps); err != nil {
		fmt.Fprintf(os.Stderr, "  %s seccomp: %v\n", sWarn.Render("!"), err)
	}

	// 3. Load and compile customer restrictions
	restrictions, err := sandbox.LoadRestrictions()
	if err != nil {
		return fmt.Errorf("restrictions: %w", err)
	}
	if warnings := restrictions.Validate(); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  %s %s\n", sWarn.Render("!"), w)
		}
	}

	plan := sandbox.Compile(restrictions, caps)
	printEnforcement(plan)

	// 4. Apply Landlock filesystem restrictions (if available)
	if err := sandbox.ApplyLandlock(plan, caps); err != nil {
		fmt.Fprintf(os.Stderr, "  %s landlock: %v\n", sWarn.Render("!"), err)
	}

	// 5. Initialize audit log (silent — no startup output)
	auditLog := sandbox.NewAuditLog()
	defer auditLog.Close()
	for _, a := range plan.Applied {
		auditLog.LogApplied(a.Category, a.Description, a.Level)
	}

	// 6. Create sandbox components
	runner := sandbox.NewIsolatedRunner(plan, caps)
	pathChecker := sandbox.NewPathChecker(plan)

	// 7. Load config (with first-run detection)
	cfg, err := LoadConfig()
	if err != nil {
		if errors.Is(err, ErrNoConfig) {
			fmt.Fprintln(os.Stderr, "Welcome to tc! Let's get you set up.")
			fmt.Fprintln(os.Stderr)
			if setupErr := RunSetup(); setupErr != nil {
				return fmt.Errorf("setup: %w", setupErr)
			}
			cfg, err = LoadConfig()
			if err != nil {
				return fmt.Errorf("config after setup: %w", err)
			}
		} else {
			return fmt.Errorf("config: %w", err)
		}
	}

	// First-run confirmation: env vars gave us a config, but user hasn't confirmed yet
	if !HasConfigFile() {
		if confirmErr := confirmFirstRun(cfg); confirmErr != nil {
			return confirmErr
		}
	}

	// 8. Initialize provider
	p, err := provider.New(cfg.Provider, cfg.APIKey, cfg.Model)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}

	// 9. Index project and build context
	wd, _ := os.Getwd()
	projectIndex, _ := memory.IndexProject(wd)
	memStore, _ := memory.NewStore(wd)
	ctxBuilder := memory.NewContextBuilder(projectIndex, memStore)

	// 10. Create directory context cache
	dirCache := memory.NewDirCache(memory.DirCacheConfig{})

	// 11. Create engine and register tools
	eng := engine.New(p)
	registry := tools.NewRegistry()
	tools.RegisterDefaults(registry, pathChecker, runner, auditLog, plan, dirCache)
	eng.SetRegistry(registry)
	eng.SetProjectContext(ctxBuilder.Build())

	// Print ready status
	fileCount := 0
	if projectIndex != nil {
		fileCount = projectIndex.TotalFiles
	}
	printReady(cfg.Provider, cfg.Model, len(registry.All()), fileCount)

	// 12. Start TUI
	model := ui.NewModel(eng, cfg.Model)
	model.SetToolCount(len(registry.All()))
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
