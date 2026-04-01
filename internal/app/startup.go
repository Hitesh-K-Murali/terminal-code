package app

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

// Styled output for startup — replaces raw fmt/log with clean presentation.
var (
	sTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	sOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	sWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	sDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	sVal   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
)

func printBanner() {
	fmt.Println()
	fmt.Println(sTitle.Render("  tc") + sDim.Render(" — terminal AI coding assistant"))
	fmt.Println()
}

func printCapabilities(caps sandbox.PlatformCapabilities) {
	fmt.Printf("  %s %s\n", sDim.Render("kernel"), sVal.Render(caps.KernelVersion))

	if caps.SeccompAvailable {
		fmt.Printf("  %s seccomp\n", sOK.Render("✓"))
	} else {
		fmt.Printf("  %s seccomp\n", sWarn.Render("–"))
	}

	if caps.LandlockAvailable {
		fmt.Printf("  %s landlock %s\n", sOK.Render("✓"), sDim.Render(fmt.Sprintf("(ABI v%d)", caps.LandlockABI)))
	} else {
		fmt.Printf("  %s landlock %s\n", sWarn.Render("–"), sDim.Render("(kernel < 5.13)"))
	}

	switch caps.CgroupVersion {
	case 2:
		fmt.Printf("  %s cgroups %s\n", sOK.Render("✓"), sDim.Render("v2"))
	case 1:
		fmt.Printf("  %s cgroups %s\n", sOK.Render("✓"), sDim.Render("v1"))
	default:
		fmt.Printf("  %s cgroups\n", sWarn.Render("–"))
	}

	if caps.UserNSAvailable {
		fmt.Printf("  %s namespaces\n", sOK.Render("✓"))
	} else {
		fmt.Printf("  %s namespaces\n", sWarn.Render("–"))
	}
}

func printEnforcement(plan *sandbox.EnforcementPlan) {
	fmt.Println()
	for _, a := range plan.Applied {
		icon := sOK.Render("✓")
		level := sDim.Render(a.Level.String())
		if a.Level == sandbox.EnforcementDegraded {
			icon = sWarn.Render("!")
			level = sWarn.Render("degraded")
		}
		fmt.Printf("  %s %s %s %s\n", icon, sVal.Render(a.Category), sDim.Render("→"), level)
	}

	if len(plan.Warnings) > 0 {
		fmt.Println()
		for _, w := range plan.Warnings {
			fmt.Printf("  %s %s\n", sWarn.Render("!"), sDim.Render(w))
		}
	}
}

func printReady(provider, model string, toolCount, fileCount int) {
	fmt.Println()
	fmt.Printf("  %s %s %s\n", sDim.Render("provider"), sVal.Render(FormatProvider(provider)), sDim.Render(model))
	fmt.Printf("  %s %s  %s %s\n",
		sDim.Render("tools"), sVal.Render(fmt.Sprintf("%d", toolCount)),
		sDim.Render("indexed"), sVal.Render(fmt.Sprintf("%d files", fileCount)))
	fmt.Println()
}

// confirmFirstRun shows a compact confirmation when env vars exist but no config file.
// Saves config after confirmation so this only happens once.
func confirmFirstRun(cfg *Config) error {
	fmt.Println()
	fmt.Printf("  %s %s %s\n",
		sOK.Render("✓"),
		sVal.Render("Detected API key from environment"),
		sDim.Render("("+FormatProvider(cfg.Provider)+")"))
	fmt.Printf("  %s %s %s\n",
		sDim.Render(" "),
		sDim.Render("Model:"),
		sVal.Render(cfg.Model))
	fmt.Println()
	fmt.Printf("  %s  %s\n",
		sVal.Render("Enter to continue"),
		sDim.Render("s = change settings"))
	fmt.Print("  ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "s" || input == "setup" {
		return RunSetup()
	}

	// Save config so this confirmation doesn't repeat
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  %s Could not save config: %v\n", sWarn.Render("!"), err)
		// Continue anyway — env vars still work
	} else {
		fmt.Printf("  %s Saved to %s\n", sOK.Render("✓"), sDim.Render(ConfigPath()))
	}
	fmt.Println()

	return nil
}
