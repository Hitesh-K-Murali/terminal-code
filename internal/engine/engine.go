package engine

import (
	"context"

	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
)

// Engine orchestrates the request→response→tool loop.
// Phase 1: simple chat (no tool calls).
// Phase 3: adds tool dispatch.
type Engine struct {
	provider provider.Provider
	history  []provider.Message
}

func New(p provider.Provider) *Engine {
	return &Engine{
		provider: p,
	}
}

// Send sends a user message and returns a streaming channel of events.
// The caller renders each event to the UI as it arrives.
func (e *Engine) Send(ctx context.Context, userMsg string) (<-chan provider.StreamEvent, error) {
	e.history = append(e.history, provider.Message{
		Role:    provider.RoleUser,
		Content: userMsg,
	})

	req := &provider.Request{
		Messages: e.history,
		SystemPrompt: `You are tc, a terminal-native AI coding assistant. You help developers write,
understand, and debug code directly from their terminal. Be concise, precise, and direct.
When showing code, use proper markdown code blocks with language tags.`,
	}

	ch, err := e.provider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	// Collect the full response for history in a wrapper channel
	out := make(chan provider.StreamEvent, 64)
	go func() {
		defer close(out)
		var fullText string

		for event := range ch {
			if event.Type == provider.EventText {
				fullText += event.Text
			}
			out <- event
		}

		// Add assistant response to history
		if fullText != "" {
			e.history = append(e.history, provider.Message{
				Role:    provider.RoleAssistant,
				Content: fullText,
			})
		}
	}()

	return out, nil
}

// History returns the current conversation messages.
func (e *Engine) History() []provider.Message {
	return e.history
}

// Reset clears the conversation history.
func (e *Engine) Reset() {
	e.history = nil
}
