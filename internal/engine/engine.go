package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
	"github.com/Hitesh-K-Murali/terminal-code/internal/tools"
)

// Engine orchestrates the request→response→tool loop.
// When the LLM requests a tool call, the engine executes it,
// sends the result back, and continues streaming.
type Engine struct {
	provider provider.Provider
	registry *tools.Registry
	history  []provider.Message
}

func New(p provider.Provider) *Engine {
	return &Engine{
		provider: p,
	}
}

// SetRegistry attaches the tool registry to the engine.
// Must be called before Send if tools are available.
func (e *Engine) SetRegistry(reg *tools.Registry) {
	e.registry = reg
}

// Send sends a user message and returns a streaming channel of events.
// The channel delivers text chunks, tool call notifications, and a final done event.
// Tool calls are executed automatically within the engine and the results
// are fed back to the LLM for continued generation.
func (e *Engine) Send(ctx context.Context, userMsg string) (<-chan provider.StreamEvent, error) {
	e.history = append(e.history, provider.Message{
		Role:    provider.RoleUser,
		Content: userMsg,
	})

	out := make(chan provider.StreamEvent, 64)

	go func() {
		defer close(out)
		e.streamLoop(ctx, out)
	}()

	return out, nil
}

// streamLoop runs the core LLM interaction loop:
// stream response → if tool_use → execute → append result → stream again
func (e *Engine) streamLoop(ctx context.Context, out chan<- provider.StreamEvent) {
	systemPrompt := `You are tc, a terminal-native AI coding assistant with kernel-level security enforcement.
You help developers write, understand, and debug code directly from their terminal.
Be concise, precise, and direct. When showing code, use proper markdown code blocks with language tags.

You have access to tools for reading/writing files, searching code, and running shell commands.
All tool operations are sandboxed: file access is restricted by policy, shell commands run in
isolated namespaces with limited binaries and no network access.
Use tools when you need to interact with the filesystem or run commands.`

	for {
		req := &provider.Request{
			Messages:     e.history,
			SystemPrompt: systemPrompt,
		}

		// Attach tool definitions if registry is available
		if e.registry != nil {
			req.Tools = e.registry.ToolDefinitions()
		}

		ch, err := e.provider.Stream(ctx, req)
		if err != nil {
			out <- provider.StreamEvent{Type: provider.EventError, Error: err}
			return
		}

		var fullText string
		var toolCalls []provider.ToolCall
		var usage *provider.Usage

		for event := range ch {
			switch event.Type {
			case provider.EventText:
				fullText += event.Text
				out <- event

			case provider.EventToolUse:
				if event.ToolCall != nil {
					toolCalls = append(toolCalls, *event.ToolCall)
					// Notify UI that a tool is being called
					out <- provider.StreamEvent{
						Type: provider.EventText,
						Text: fmt.Sprintf("\n[calling tool: %s]\n", event.ToolCall.Name),
					}
				}

			case provider.EventDone:
				usage = event.Usage

			case provider.EventError:
				out <- event
				return
			}
		}

		// Add assistant message to history
		if fullText != "" {
			e.history = append(e.history, provider.Message{
				Role:    provider.RoleAssistant,
				Content: fullText,
			})
		}

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			out <- provider.StreamEvent{Type: provider.EventDone, Usage: usage}
			return
		}

		// Execute tool calls and feed results back
		for _, tc := range toolCalls {
			result := e.executeTool(ctx, tc)

			// Notify UI of tool result
			out <- provider.StreamEvent{
				Type: provider.EventText,
				Text: fmt.Sprintf("[tool result: %s → %d chars]\n", tc.Name, len(result)),
			}

			// Add tool result to history as user message
			// (Anthropic format: tool_result is sent as a user message)
			e.history = append(e.history, provider.Message{
				Role:    provider.RoleUser,
				Content: fmt.Sprintf("[Tool result for %s (id: %s)]:\n%s", tc.Name, tc.ID, result),
			})
		}

		// Continue the loop — the LLM will see the tool results and generate more
	}
}

// executeTool runs a single tool call and returns the result string.
func (e *Engine) executeTool(ctx context.Context, tc provider.ToolCall) string {
	if e.registry == nil {
		return fmt.Sprintf("error: no tools registered")
	}

	tool, ok := e.registry.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", tc.Name)
	}

	result, err := tool.Execute(ctx, tc.Input)
	if err != nil {
		log.Printf("tool %s error: %v", tc.Name, err)
		return fmt.Sprintf("error: %v", err)
	}

	// Truncate very large results
	const maxResultLen = 50000
	if len(result) > maxResultLen {
		result = result[:maxResultLen] + "\n... [truncated]"
	}

	return result
}

// History returns the current conversation messages.
func (e *Engine) History() []provider.Message {
	return e.history
}

// Reset clears the conversation history.
func (e *Engine) Reset() {
	e.history = nil
}

// ToolDefinitions returns the tool definitions as JSON (for debugging/display).
func (e *Engine) ToolDefinitions() json.RawMessage {
	if e.registry == nil {
		return nil
	}
	return e.registry.ToolDefinitions()
}
