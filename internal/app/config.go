package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Provider string `toml:"provider"`
	APIKey   string `toml:"api_key"`
	Model    string `toml:"model"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
	}

	// Try config file first
	home, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(home, ".tc", "config.toml")
		if data, err := os.ReadFile(configPath); err == nil {
			if err := toml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse %s: %w", configPath, err)
			}
		}
	}

	// Environment variables override config file
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
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

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key found. Set ANTHROPIC_API_KEY or OPENAI_API_KEY, or configure ~/.tc/config.toml")
	}

	return cfg, nil
}
