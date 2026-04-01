package app

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	setupTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7C3AED")).
			Bold(true).
			MarginBottom(1)

	setupOK = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true)

	setupMuted = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))
)

// RunSetup runs the interactive first-time configuration wizard.
func RunSetup() error {
	fmt.Println(setupTitle.Render("  tc setup"))
	fmt.Println()

	var (
		providerChoice string
		apiKey         string
		modelChoice    string
	)

	// Step 1: Provider selection
	providerForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which AI provider do you want to use?").
				Options(
					huh.NewOption("Anthropic (Claude)", "anthropic"),
					huh.NewOption("OpenAI (GPT)", "openai"),
					huh.NewOption("Ollama (local — free, no API key)", "ollama"),
				).
				Value(&providerChoice),
		),
	)

	if err := providerForm.Run(); err != nil {
		return fmt.Errorf("provider selection: %w", err)
	}

	// Step 2: API key (skip for Ollama)
	if ProviderNeedsKey(providerChoice) {
		// Check env first
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}

		if apiKey != "" {
			fmt.Printf("  %s API key detected from environment\n\n",
				setupOK.Render("✓"))
		} else {
			keyForm := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title(fmt.Sprintf("%s API Key", FormatProvider(providerChoice))).
						Description("Paste your API key (it will be stored in ~/.tc/config.toml with 0600 permissions)").
						EchoMode(huh.EchoModePassword).
						Value(&apiKey),
				),
			)

			if err := keyForm.Run(); err != nil {
				return fmt.Errorf("api key input: %w", err)
			}

			if apiKey == "" {
				fmt.Println(setupMuted.Render("  No API key entered. You can set it later with: tc config set api_key <key>"))
			}
		}
	} else {
		apiKey = "ollama"
	}

	// Step 3: Model selection
	models := DefaultModels(providerChoice)
	modelChoice = DefaultModel(providerChoice)

	if len(models) > 0 {
		modelOptions := make([]huh.Option[string], len(models))
		for i, m := range models {
			label := m
			if m == modelChoice {
				label += " (recommended)"
			}
			modelOptions[i] = huh.NewOption(label, m)
		}

		modelForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which model?").
					Options(modelOptions...).
					Value(&modelChoice),
			),
		)

		if err := modelForm.Run(); err != nil {
			return fmt.Errorf("model selection: %w", err)
		}
	}

	// Step 4: Save config
	cfg := &Config{
		Provider: providerChoice,
		APIKey:   apiKey,
		Model:    modelChoice,
	}

	// Preserve existing base URL from env
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
		cfg.BaseURL = baseURL
	}

	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Create subdirectories
	EnsureSubdirs()

	// Success
	fmt.Println()
	fmt.Printf("  %s Config saved to %s\n",
		setupOK.Render("✓"), ConfigPath())
	fmt.Printf("  %s Provider: %s\n",
		setupOK.Render("✓"), FormatProvider(cfg.Provider))
	fmt.Printf("  %s Model: %s\n",
		setupOK.Render("✓"), cfg.Model)
	fmt.Println()

	return nil
}
