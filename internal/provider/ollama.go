package provider

import (
	"fmt"
)

// Ollama implements Provider for local models via Ollama.
// Ollama exposes an OpenAI-compatible API at localhost:11434/v1,
// so we reuse the OpenAI provider with a different base URL.
type Ollama struct {
	*OpenAI
}

func NewOllama(model string) (*Ollama, error) {
	if model == "" {
		model = "llama3.2"
	}

	inner := &OpenAI{
		apiKey:  "ollama", // Ollama doesn't require a real API key
		model:   model,
		baseURL: "http://localhost:11434/v1",
	}

	return &Ollama{OpenAI: inner}, nil
}

func (o *Ollama) Name() string { return "ollama" }

func (o *Ollama) Models() []string {
	return []string{
		"llama3.2",
		"llama3.1",
		"codellama",
		"deepseek-coder-v2",
		"qwen2.5-coder",
		"mistral",
	}
}

// NewOllamaWithURL creates an Ollama provider connecting to a custom endpoint.
func NewOllamaWithURL(model, baseURL string) (*Ollama, error) {
	if model == "" {
		model = "llama3.2"
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	inner := &OpenAI{
		apiKey:  "ollama",
		model:   model,
		baseURL: baseURL,
	}

	return &Ollama{OpenAI: inner}, nil
}

func init() {
	// Ensure Ollama satisfies Provider at compile time
	var _ Provider = (*Ollama)(nil)
	_ = fmt.Sprintf // suppress unused import
}
