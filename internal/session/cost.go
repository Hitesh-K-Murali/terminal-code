package session

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// ModelPricing defines per-model token costs in USD per million tokens.
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// Known model pricing (as of 2026)
var modelPricing = map[string]ModelPricing{
	// Anthropic
	"claude-opus-4-20250514":   {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-sonnet-4-20250514": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-haiku-4-20250414":  {InputPerMillion: 0.25, OutputPerMillion: 1.25},
	// OpenAI
	"gpt-4o":      {InputPerMillion: 2.50, OutputPerMillion: 10.0},
	"gpt-4o-mini": {InputPerMillion: 0.15, OutputPerMillion: 0.60},
	"o1":          {InputPerMillion: 15.0, OutputPerMillion: 60.0},
	"o3-mini":     {InputPerMillion: 1.10, OutputPerMillion: 4.40},
	// Ollama (local — free)
	"llama3.2":           {InputPerMillion: 0, OutputPerMillion: 0},
	"llama3.1":           {InputPerMillion: 0, OutputPerMillion: 0},
	"codellama":          {InputPerMillion: 0, OutputPerMillion: 0},
	"deepseek-coder-v2":  {InputPerMillion: 0, OutputPerMillion: 0},
	"qwen2.5-coder":     {InputPerMillion: 0, OutputPerMillion: 0},
	"mistral":            {InputPerMillion: 0, OutputPerMillion: 0},
}

// CostTracker tracks token usage and cost in real-time.
type CostTracker struct {
	model        string
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	costMu       sync.Mutex
	totalCost    float64
	requests     atomic.Int64
	toolCalls    atomic.Int64
}

func NewCostTracker(model string) *CostTracker {
	return &CostTracker{model: model}
}

// Record adds a completed request's usage to the tracker.
func (ct *CostTracker) Record(inputTokens, outputTokens int) {
	ct.inputTokens.Add(int64(inputTokens))
	ct.outputTokens.Add(int64(outputTokens))
	ct.requests.Add(1)

	cost := ct.calculateCost(inputTokens, outputTokens)
	ct.costMu.Lock()
	ct.totalCost += cost
	ct.costMu.Unlock()
}

// RecordToolCall increments the tool call counter.
func (ct *CostTracker) RecordToolCall() {
	ct.toolCalls.Add(1)
}

// SetModel updates the model for cost calculation.
func (ct *CostTracker) SetModel(model string) {
	ct.model = model
}

func (ct *CostTracker) calculateCost(input, output int) float64 {
	pricing, ok := modelPricing[ct.model]
	if !ok {
		// Default estimate
		pricing = ModelPricing{InputPerMillion: 3.0, OutputPerMillion: 15.0}
	}
	return float64(input)*pricing.InputPerMillion/1_000_000 +
		float64(output)*pricing.OutputPerMillion/1_000_000
}

// InputTokens returns total input tokens used.
func (ct *CostTracker) InputTokens() int64 { return ct.inputTokens.Load() }

// OutputTokens returns total output tokens used.
func (ct *CostTracker) OutputTokens() int64 { return ct.outputTokens.Load() }

// TotalCost returns total cost in USD.
func (ct *CostTracker) TotalCost() float64 {
	ct.costMu.Lock()
	defer ct.costMu.Unlock()
	return ct.totalCost
}

// Requests returns total API requests made.
func (ct *CostTracker) Requests() int64 { return ct.requests.Load() }

// ToolCalls returns total tool calls executed.
func (ct *CostTracker) ToolCalls() int64 { return ct.toolCalls.Load() }

// Summary returns a human-readable cost summary.
func (ct *CostTracker) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Model: %s\n", ct.model))
	sb.WriteString(fmt.Sprintf("Requests: %d\n", ct.Requests()))
	sb.WriteString(fmt.Sprintf("Tool calls: %d\n", ct.ToolCalls()))
	sb.WriteString(fmt.Sprintf("Input tokens: %d\n", ct.InputTokens()))
	sb.WriteString(fmt.Sprintf("Output tokens: %d\n", ct.OutputTokens()))
	sb.WriteString(fmt.Sprintf("Total tokens: %d\n", ct.InputTokens()+ct.OutputTokens()))
	sb.WriteString(fmt.Sprintf("Estimated cost: $%.4f\n", ct.TotalCost()))

	pricing, ok := modelPricing[ct.model]
	if ok {
		sb.WriteString(fmt.Sprintf("Pricing: $%.2f/M input, $%.2f/M output",
			pricing.InputPerMillion, pricing.OutputPerMillion))
	}

	return sb.String()
}

// StatusLine returns a compact one-liner for the status bar.
func (ct *CostTracker) StatusLine() string {
	return fmt.Sprintf("%d/%d tok  $%.4f",
		ct.InputTokens(), ct.OutputTokens(), ct.TotalCost())
}
