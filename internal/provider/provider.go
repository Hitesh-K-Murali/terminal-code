package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// Role constants for messages
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// StreamEventType identifies what kind of data a stream event carries.
type StreamEventType int

const (
	EventText      StreamEventType = iota // Text content delta
	EventToolUse                          // Tool call request
	EventDone                             // Stream complete
	EventError                            // Error occurred
)

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is sent to a provider to generate a response.
type Request struct {
	Messages     []Message       `json:"messages"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Model        string          `json:"model,omitempty"`
	MaxTokens    int             `json:"max_tokens,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
}

// Response is the complete result from a non-streaming call.
type Response struct {
	Content    string          `json:"content"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	Usage      Usage           `json:"usage"`
	StopReason string          `json:"stop_reason"`
	Raw        json.RawMessage `json:"-"`
}

// ToolCall represents the LLM requesting a tool execution.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent is a single chunk from a streaming response.
type StreamEvent struct {
	Type     StreamEventType
	Text     string    // For EventText
	ToolCall *ToolCall // For EventToolUse
	Usage    *Usage    // For EventDone
	Error    error     // For EventError
}

// Provider is the interface every LLM backend implements.
// The engine never knows which LLM it's talking to.
type Provider interface {
	// Stream sends a request and returns a channel of streaming events.
	// The channel is closed when the response is complete or an error occurs.
	Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)

	// Complete sends a request and waits for the full response.
	Complete(ctx context.Context, req *Request) (*Response, error)

	// Name returns the provider identifier (e.g., "anthropic", "openai", "ollama").
	Name() string

	// Models returns the list of available models for this provider.
	Models() []string
}

// New creates a provider by name.
func New(name, apiKey, model string) (Provider, error) {
	switch name {
	case "anthropic":
		return NewAnthropic(apiKey, model)
	case "openai":
		return NewOpenAI(apiKey, model)
	case "ollama":
		return NewOllama(model)
	default:
		return nil, fmt.Errorf("unknown provider: %q (available: anthropic, openai, ollama)", name)
	}
}
