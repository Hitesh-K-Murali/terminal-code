package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAI implements Provider for OpenAI-compatible APIs (GPT, etc.).
// Uses raw HTTP to avoid the heavy official SDK dependency.
type OpenAI struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenAI(apiKey, model string) (*OpenAI, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai API key required")
	}
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAI{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.openai.com/v1",
		client:  &http.Client{},
	}, nil
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Models() []string {
	return []string{"gpt-4o", "gpt-4o-mini", "o1", "o3-mini"}
}

func (o *OpenAI) Complete(ctx context.Context, req *Request) (*Response, error) {
	body := o.buildRequestBody(req, false)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai decode: %w", err)
	}

	return o.convertResponse(result), nil
}

func (o *OpenAI) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	body := o.buildRequestBody(req, true)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamEvent, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		decoder := newSSEDecoder(resp.Body)
		var toolCalls map[int]*ToolCall // Accumulate tool call deltas by index

		for {
			line, err := decoder.Next()
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					break
				}
				ch <- StreamEvent{Type: EventError, Error: err}
				return
			}

			if line == "[DONE]" {
				break
			}

			var chunk openaiStreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}

			for _, choice := range chunk.Choices {
				delta := choice.Delta

				// Text content
				if delta.Content != "" {
					ch <- StreamEvent{Type: EventText, Text: delta.Content}
				}

				// Tool calls (accumulated across deltas)
				for _, tc := range delta.ToolCalls {
					if toolCalls == nil {
						toolCalls = make(map[int]*ToolCall)
					}
					existing, ok := toolCalls[tc.Index]
					if !ok {
						existing = &ToolCall{
							ID:   tc.ID,
							Name: tc.Function.Name,
						}
						toolCalls[tc.Index] = existing
					}
					if tc.Function.Name != "" {
						existing.Name = tc.Function.Name
					}
					if tc.ID != "" {
						existing.ID = tc.ID
					}
					// Accumulate argument JSON fragments
					existing.Input = appendJSON(existing.Input, tc.Function.Arguments)
				}

				if choice.FinishReason == "tool_calls" {
					// Emit accumulated tool calls
					for _, tc := range toolCalls {
						ch <- StreamEvent{Type: EventToolUse, ToolCall: tc}
					}
					toolCalls = nil
				}
			}

			if chunk.Usage != nil {
				ch <- StreamEvent{
					Type: EventDone,
					Usage: &Usage{
						InputTokens:  chunk.Usage.PromptTokens,
						OutputTokens: chunk.Usage.CompletionTokens,
					},
				}
			}
		}

		ch <- StreamEvent{Type: EventDone}
	}()

	return ch, nil
}

func (o *OpenAI) buildRequestBody(req *Request, stream bool) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+1)

	if req.SystemPrompt != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	model := req.Model
	if model == "" {
		model = o.model
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body["max_completion_tokens"] = maxTokens

	// Convert tools if present
	if len(req.Tools) > 0 {
		body["tools"] = convertOpenAITools(req.Tools)
	}

	return body
}

func convertOpenAITools(toolsJSON json.RawMessage) []map[string]any {
	type toolDef struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}

	var defs []toolDef
	if err := json.Unmarshal(toolsJSON, &defs); err != nil {
		return nil
	}

	out := make([]map[string]any, len(defs))
	for i, d := range defs {
		out[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        d.Name,
				"description": d.Description,
				"parameters":  json.RawMessage(d.InputSchema),
			},
		}
	}
	return out
}

func (o *OpenAI) convertResponse(resp openaiResponse) *Response {
	r := &Response{}
	if len(resp.Choices) > 0 {
		r.Content = resp.Choices[0].Message.Content
		r.StopReason = resp.Choices[0].FinishReason

		for _, tc := range resp.Choices[0].Message.ToolCalls {
			r.ToolCalls = append(r.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			})
		}
	}
	if resp.Usage != nil {
		r.Usage = Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}
	return r
}

func appendJSON(existing json.RawMessage, fragment string) json.RawMessage {
	if fragment == "" {
		return existing
	}
	if len(existing) == 0 {
		return json.RawMessage(fragment)
	}
	return json.RawMessage(string(existing) + fragment)
}

// OpenAI API response types

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

type openaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
	Index    int            `json:"index"`
}

type openaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openaiStreamChunk struct {
	Choices []openaiStreamChoice `json:"choices"`
	Usage   *openaiUsage         `json:"usage"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   string           `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

// SSE decoder for streaming responses

type sseDecoder struct {
	reader io.Reader
	buf    []byte
}

func newSSEDecoder(r io.Reader) *sseDecoder {
	return &sseDecoder{reader: r, buf: make([]byte, 0, 4096)}
}

func (d *sseDecoder) Next() (string, error) {
	// Read until we find a "data: " line
	for {
		// Read more data
		tmp := make([]byte, 4096)
		n, err := d.reader.Read(tmp)
		if n > 0 {
			d.buf = append(d.buf, tmp[:n]...)
		}

		// Try to extract a complete SSE event
		for {
			idx := indexOf(d.buf, '\n')
			if idx < 0 {
				break
			}

			line := strings.TrimSpace(string(d.buf[:idx]))
			d.buf = d.buf[idx+1:]

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				return data, nil
			}
		}

		if err != nil {
			return "", err
		}
	}
}

func indexOf(buf []byte, b byte) int {
	for i, v := range buf {
		if v == b {
			return i
		}
	}
	return -1
}
