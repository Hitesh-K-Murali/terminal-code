package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/liushuangls/go-anthropic/v2"
)

// Anthropic implements Provider for Claude models.
type Anthropic struct {
	client *anthropic.Client
	model  string
}

func NewAnthropic(apiKey, model string) (*Anthropic, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic API key required")
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// Support custom base URL (e.g., corporate proxy)
	// go-anthropic appends "/messages" to the base URL, so if the proxy
	// expects /v1/messages, set ANTHROPIC_BASE_URL to https://host/v1
	var opts []anthropic.ClientOption
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
		// Ensure the base URL includes the /v1 path if needed
		if !strings.HasSuffix(baseURL, "/v1") && !strings.HasSuffix(baseURL, "/v1/") {
			baseURL = strings.TrimRight(baseURL, "/") + "/v1"
		}
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}

	// Support auth token (alternative to API key)
	if authToken := os.Getenv("ANTHROPIC_AUTH_TOKEN"); authToken != "" {
		apiKey = authToken
	}

	client := anthropic.NewClient(apiKey, opts...)
	return &Anthropic{client: client, model: model}, nil
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Models() []string {
	return []string{
		"claude-opus-4-20250514",
		"claude-sonnet-4-20250514",
		"claude-haiku-4-20250414",
	}
}

func (a *Anthropic) Complete(ctx context.Context, req *Request) (*Response, error) {
	msgs := convertMessages(req.Messages)

	apiReq := anthropic.MessagesRequest{
		Model:     anthropic.Model(a.resolveModel(req.Model)),
		Messages:  msgs,
		MaxTokens: a.resolveMaxTokens(req.MaxTokens),
		System:    req.SystemPrompt,
		Tools:     convertToolDefs(req.Tools),
	}

	resp, err := a.client.CreateMessages(ctx, apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic complete: %w", err)
	}

	return a.convertResponse(resp), nil
}

func (a *Anthropic) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	msgs := convertMessages(req.Messages)

	ch := make(chan StreamEvent, 64)
	var once sync.Once

	streamReq := anthropic.MessagesStreamRequest{
		MessagesRequest: anthropic.MessagesRequest{
			Model:     anthropic.Model(a.resolveModel(req.Model)),
			Messages:  msgs,
			MaxTokens: a.resolveMaxTokens(req.MaxTokens),
			System:    req.SystemPrompt,
			Tools:     convertToolDefs(req.Tools),
		},

		OnContentBlockDelta: func(data anthropic.MessagesEventContentBlockDeltaData) {
			if data.Delta.Text != nil && *data.Delta.Text != "" {
				ch <- StreamEvent{
					Type: EventText,
					Text: *data.Delta.Text,
				}
			}
			// Tool input JSON deltas are accumulated by the library and
			// delivered as a complete block in OnContentBlockStop
		},

		OnContentBlockStop: func(_ anthropic.MessagesEventContentBlockStopData, content anthropic.MessageContent) {
			// When a tool_use block completes, emit a ToolUse event
			if content.Type == anthropic.MessagesContentTypeToolUse && content.MessageContentToolUse != nil {
				ch <- StreamEvent{
					Type: EventToolUse,
					ToolCall: &ToolCall{
						ID:    content.MessageContentToolUse.ID,
						Name:  content.MessageContentToolUse.Name,
						Input: content.MessageContentToolUse.Input,
					},
				}
			}
		},

		OnMessageDelta: func(data anthropic.MessagesEventMessageDeltaData) {
			once.Do(func() {
				ch <- StreamEvent{
					Type: EventDone,
					Usage: &Usage{
						OutputTokens: int(data.Usage.OutputTokens),
					},
				}
			})
		},

		OnError: func(errResp anthropic.ErrorResponse) {
			errMsg := errResp.Type
			if errResp.Error != nil {
				errMsg = errResp.Error.Error()
			}
			ch <- StreamEvent{
				Type:  EventError,
				Error: fmt.Errorf("anthropic: %s", errMsg),
			}
		},
	}

	go func() {
		defer close(ch)

		_, err := a.client.CreateMessagesStream(ctx, streamReq)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			ch <- StreamEvent{
				Type:  EventError,
				Error: fmt.Errorf("anthropic stream: %w", err),
			}
			return
		}

		once.Do(func() {
			ch <- StreamEvent{Type: EventDone}
		})
	}()

	return ch, nil
}

func (a *Anthropic) resolveModel(override string) string {
	if override != "" {
		return override
	}
	return a.model
}

func (a *Anthropic) resolveMaxTokens(override int) int {
	if override > 0 {
		return override
	}
	return 4096
}

func convertMessages(msgs []Message) []anthropic.Message {
	out := make([]anthropic.Message, len(msgs))
	for i, m := range msgs {
		role := anthropic.RoleUser
		if m.Role == RoleAssistant {
			role = anthropic.RoleAssistant
		}
		out[i] = anthropic.Message{
			Role: role,
			Content: []anthropic.MessageContent{
				anthropic.NewTextMessageContent(m.Content),
			},
		}
	}
	return out
}

// convertToolDefs converts our JSON tool definitions to Anthropic's format.
func convertToolDefs(toolsJSON json.RawMessage) []anthropic.ToolDefinition {
	if len(toolsJSON) == 0 {
		return nil
	}

	type toolDef struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}

	var defs []toolDef
	if err := json.Unmarshal(toolsJSON, &defs); err != nil {
		return nil
	}

	out := make([]anthropic.ToolDefinition, len(defs))
	for i, d := range defs {
		out[i] = anthropic.ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	return out
}

func (a *Anthropic) convertResponse(resp anthropic.MessagesResponse) *Response {
	var content string
	var toolCalls []ToolCall

	for _, block := range resp.Content {
		if block.Text != nil {
			content += *block.Text
		}
		if block.Type == anthropic.MessagesContentTypeToolUse && block.MessageContentToolUse != nil {
			toolCalls = append(toolCalls, ToolCall{
				ID:    block.MessageContentToolUse.ID,
				Name:  block.MessageContentToolUse.Name,
				Input: block.MessageContentToolUse.Input,
			})
		}
	}

	return &Response{
		Content:    content,
		ToolCalls:  toolCalls,
		Usage: Usage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
		},
		StopReason: string(resp.StopReason),
	}
}
