package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ErrNoConfig is returned when no config file exists and no env vars are set.
// Callers use this to trigger the first-run setup wizard.
var ErrNoConfig = errors.New("no configuration found")

type Config struct {
	Provider  string `toml:"provider"`
	APIKey    string `toml:"api_key"`
	Model     string `toml:"model"`
	BaseURL   string `toml:"base_url,omitempty"`
	OllamaURL string `toml:"ollama_url,omitempty"`
}

// ConfigDir returns ~/.tc, creating it if absent.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tc"
	}
	dir := filepath.Join(home, ".tc")
	os.MkdirAll(dir, 0700)
	return dir
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// ConfigExists checks whether the config file exists.
func ConfigExists() bool {
	_, err := os.Stat(ConfigPath())
	return err == nil
}

// LoadConfig loads configuration from file and environment.
// Returns ErrNoConfig if no config file exists AND no env vars are set.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
	}

	fileFound := false
	path := ConfigPath()
	if data, err := os.ReadFile(path); err == nil {
		fileFound = true
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}

	// Environment variables override config file
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.APIKey = key
		if cfg.Provider == "" || cfg.Provider == "anthropic" {
			cfg.Provider = "anthropic"
		}
	}
	if key := os.Getenv("ANTHROPIC_AUTH_TOKEN"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = "anthropic"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = "openai"
	}
	if model := os.Getenv("TC_MODEL"); model != "" {
		cfg.Model = model
	}
	if provider := os.Getenv("TC_PROVIDER"); provider != "" {
		cfg.Provider = provider
	}
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" && cfg.BaseURL == "" {
		cfg.BaseURL = baseURL
	}

	if cfg.APIKey == "" {
		if !fileFound {
			return nil, ErrNoConfig
		}
		// Ollama doesn't need an API key
		if cfg.Provider == "ollama" {
			cfg.APIKey = "ollama"
			return cfg, nil
		}
		return nil, fmt.Errorf("no API key found. Set ANTHROPIC_API_KEY or OPENAI_API_KEY, or run: tc setup")
	}

	return cfg, nil
}

// SaveConfig writes config atomically to ~/.tc/config.toml.
// Uses temp file + rename for crash safety. File permissions 0600 (contains API key).
func SaveConfig(cfg *Config) error {
	dir := ConfigDir()
	os.MkdirAll(dir, 0700)

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	path := ConfigPath()
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("save config: %w", err)
	}

	return nil
}

// ValidateConfig returns a list of issues with the config.
func ValidateConfig(cfg *Config) []string {
	var issues []string

	knownProviders := map[string]bool{"anthropic": true, "openai": true, "ollama": true}
	if !knownProviders[cfg.Provider] {
		issues = append(issues, fmt.Sprintf("unknown provider %q (expected: anthropic, openai, ollama)", cfg.Provider))
	}

	if cfg.Provider != "ollama" && cfg.APIKey == "" {
		issues = append(issues, fmt.Sprintf("provider %q requires an API key", cfg.Provider))
	}

	if cfg.Model == "" {
		issues = append(issues, "model is empty")
	}

	return issues
}

// RunConfigShow prints the current config with the API key masked.
func RunConfigShow() error {
	cfg, err := LoadConfig()
	if err != nil {
		if errors.Is(err, ErrNoConfig) {
			fmt.Println("No config found. Run: tc setup")
			return nil
		}
		return err
	}

	fmt.Printf("provider  = %q\n", cfg.Provider)
	fmt.Printf("api_key   = %q\n", maskKey(cfg.APIKey))
	fmt.Printf("model     = %q\n", cfg.Model)
	if cfg.BaseURL != "" {
		fmt.Printf("base_url  = %q\n", cfg.BaseURL)
	}
	if cfg.OllamaURL != "" {
		fmt.Printf("ollama_url = %q\n", cfg.OllamaURL)
	}
	fmt.Printf("\nConfig file: %s\n", ConfigPath())
	return nil
}

// RunConfigGet prints a single config value.
func RunConfigGet(key string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	switch key {
	case "provider":
		fmt.Println(cfg.Provider)
	case "api_key":
		fmt.Println(maskKey(cfg.APIKey))
	case "model":
		fmt.Println(cfg.Model)
	case "base_url":
		fmt.Println(cfg.BaseURL)
	case "ollama_url":
		fmt.Println(cfg.OllamaURL)
	default:
		return fmt.Errorf("unknown config key %q (available: provider, api_key, model, base_url, ollama_url)", key)
	}
	return nil
}

// RunConfigSet updates a single config value.
func RunConfigSet(key, value string) error {
	cfg, err := LoadConfig()
	if err != nil {
		if errors.Is(err, ErrNoConfig) {
			cfg = &Config{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}
		} else {
			return err
		}
	}

	switch key {
	case "provider":
		cfg.Provider = value
	case "api_key":
		cfg.APIKey = value
	case "model":
		cfg.Model = value
	case "base_url":
		cfg.BaseURL = value
	case "ollama_url":
		cfg.OllamaURL = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}

	if err := SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Set %s = %q\n", key, value)
	return nil
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// EnsureSubdirs creates the standard subdirectories under ~/.tc/.
func EnsureSubdirs() {
	dir := ConfigDir()
	for _, sub := range []string{"sessions", "audit", "memory"} {
		os.MkdirAll(filepath.Join(dir, sub), 0700)
	}
}

// DefaultModels returns available models for each provider.
func DefaultModels(provider string) []string {
	switch provider {
	case "anthropic":
		return []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-haiku-4-20250414"}
	case "openai":
		return []string{"gpt-4o", "gpt-4o-mini", "o1", "o3-mini"}
	case "ollama":
		return []string{"llama3.2", "llama3.1", "codellama", "deepseek-coder-v2", "qwen2.5-coder", "mistral"}
	default:
		return nil
	}
}

// DefaultModel returns the default model for a provider.
func DefaultModel(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-20250514"
	case "openai":
		return "gpt-4o"
	case "ollama":
		return "llama3.2"
	default:
		return ""
	}
}

// ProviderNeedsKey returns true if the provider requires an API key.
func ProviderNeedsKey(provider string) bool {
	return provider != "ollama"
}

// FormatProvider returns a display name for a provider.
func FormatProvider(provider string) string {
	switch provider {
	case "anthropic":
		return "Anthropic (Claude)"
	case "openai":
		return "OpenAI (GPT)"
	case "ollama":
		return "Ollama (local)"
	default:
		return strings.Title(provider)
	}
}
