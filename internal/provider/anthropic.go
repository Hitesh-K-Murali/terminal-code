package provider

import (
	"context"
	"fmt"
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

	client := anthropic.NewClient(apiKey)
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

	// go-anthropic v2 uses callback-based streaming.
	// We bridge callbacks → channel for our Provider interface.
	var once sync.Once

	streamReq := anthropic.MessagesStreamRequest{
		MessagesRequest: anthropic.MessagesRequest{
			Model:     anthropic.Model(a.resolveModel(req.Model)),
			Messages:  msgs,
			MaxTokens: a.resolveMaxTokens(req.MaxTokens),
			System:    req.SystemPrompt,
		},

		OnContentBlockDelta: func(data anthropic.MessagesEventContentBlockDeltaData) {
			if data.Delta.Text != nil && *data.Delta.Text != "" {
				ch <- StreamEvent{
					Type: EventText,
					Text: *data.Delta.Text,
				}
			}
		},

		OnMessageDelta: func(data anthropic.MessagesEventMessageDeltaData) {
			// Message delta carries usage info
			once.Do(func() {
				ch <- StreamEvent{
					Type: EventDone,
					Usage: &Usage{
						InputTokens:  0, // Input tokens come from message_start
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
				return // Context cancelled, don't send error
			}
			ch <- StreamEvent{
				Type:  EventError,
				Error: fmt.Errorf("anthropic stream: %w", err),
			}
			return
		}

		// Ensure done is sent even if OnMessageDelta didn't fire
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

func (a *Anthropic) convertResponse(resp anthropic.MessagesResponse) *Response {
	var content string
	for _, block := range resp.Content {
		if block.Text != nil {
			content += *block.Text
		}
	}

	return &Response{
		Content: content,
		Usage: Usage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
		},
		StopReason: string(resp.StopReason),
	}
}
