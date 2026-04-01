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
	"github.com/Hitesh-K-Murali/terminal-code/internal/memory"
	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
	"github.com/Hitesh-K-Murali/terminal-code/internal/tools"
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

	// 3. Load and compile customer restrictions
	restrictions, err := sandbox.LoadRestrictions()
	if err != nil {
		return fmt.Errorf("restrictions: %w", err)
	}
	if warnings := restrictions.Validate(); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  restriction warning: %s\n", w)
		}
	}

	plan := sandbox.Compile(restrictions, caps)
	plan.Report()

	// 4. Apply Landlock filesystem restrictions (if available)
	if err := sandbox.ApplyLandlock(plan, caps); err != nil {
		log.Printf("warning: landlock: %v", err)
	}

	// 5. Initialize audit log
	auditLog := sandbox.NewAuditLog()
	defer auditLog.Close()

	// Log all applied restrictions
	for _, a := range plan.Applied {
		auditLog.LogApplied(a.Category, a.Description, a.Level)
	}

	// 6. Create isolated runner for subprocess execution
	runner := sandbox.NewIsolatedRunner(plan, caps)

	// 7. Create path checker (app-level filesystem enforcement)
	pathChecker := sandbox.NewPathChecker(plan)

	// 8. Load config
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 9. Initialize provider
	p, err := provider.New(cfg.Provider, cfg.APIKey, cfg.Model)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}

	// 10. Index project and build context
	wd, _ := os.Getwd()
	projectIndex, err := memory.IndexProject(wd)
	if err != nil {
		log.Printf("warning: project indexing: %v", err)
	} else {
		fmt.Fprintf(os.Stderr, "  project: indexed %d files\n", projectIndex.TotalFiles)
	}

	memStore, _ := memory.NewStore(wd)
	ctxBuilder := memory.NewContextBuilder(projectIndex, memStore)

	// 11. Create directory context cache
	dirCache := memory.NewDirCache(memory.DirCacheConfig{
		PersistToDisk: false, // In-memory only by default
	})

	// 12. Create engine and register tools
	eng := engine.New(p)

	registry := tools.NewRegistry()
	tools.RegisterDefaults(registry, pathChecker, runner, auditLog, plan, dirCache)
	eng.SetRegistry(registry)

	// Inject project context (compact reference, not full content)
	eng.SetProjectContext(ctxBuilder.Build())

	// 11. Start TUI
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
