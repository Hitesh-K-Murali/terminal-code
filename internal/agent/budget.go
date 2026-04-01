package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Budget tracks resource consumption for a single agent or session.
type Budget struct {
	MaxTokens     int
	MaxCostUSD    float64
	MaxFilesWrite int

	tokensUsed   atomic.Int64
	costUsedMu   sync.Mutex
	costUsed     float64
	filesWritten atomic.Int32
}

func NewBudget(maxTokens int, maxCostUSD float64, maxFiles int) *Budget {
	return &Budget{
		MaxTokens:     maxTokens,
		MaxCostUSD:    maxCostUSD,
		MaxFilesWrite: maxFiles,
	}
}

// RecordTokens adds tokens used and returns an error if budget exceeded.
func (b *Budget) RecordTokens(input, output int) error {
	total := int64(input + output)
	newTotal := b.tokensUsed.Add(total)

	if b.MaxTokens > 0 && newTotal > int64(b.MaxTokens) {
		return fmt.Errorf("token budget exceeded: %d / %d", newTotal, b.MaxTokens)
	}
	return nil
}

// RecordCost adds cost and returns an error if budget exceeded.
func (b *Budget) RecordCost(costUSD float64) error {
	b.costUsedMu.Lock()
	b.costUsed += costUSD
	current := b.costUsed
	b.costUsedMu.Unlock()

	if b.MaxCostUSD > 0 && current > b.MaxCostUSD {
		return fmt.Errorf("cost budget exceeded: $%.4f / $%.2f", current, b.MaxCostUSD)
	}
	return nil
}

// RecordFileWrite increments the file write counter.
func (b *Budget) RecordFileWrite() error {
	newCount := b.filesWritten.Add(1)
	if b.MaxFilesWrite > 0 && int(newCount) > b.MaxFilesWrite {
		return fmt.Errorf("file write budget exceeded: %d / %d", newCount, b.MaxFilesWrite)
	}
	return nil
}

// TokensUsed returns total tokens consumed.
func (b *Budget) TokensUsed() int64 {
	return b.tokensUsed.Load()
}

// CostUsed returns total cost in USD.
func (b *Budget) CostUsed() float64 {
	b.costUsedMu.Lock()
	defer b.costUsedMu.Unlock()
	return b.costUsed
}

// FilesWritten returns number of files written.
func (b *Budget) FilesWritten() int32 {
	return b.filesWritten.Load()
}

// Exceeded returns true if any budget limit has been exceeded.
func (b *Budget) Exceeded() bool {
	if b.MaxTokens > 0 && b.tokensUsed.Load() > int64(b.MaxTokens) {
		return true
	}
	if b.MaxFilesWrite > 0 && int(b.filesWritten.Load()) > b.MaxFilesWrite {
		return true
	}
	b.costUsedMu.Lock()
	exceeded := b.MaxCostUSD > 0 && b.costUsed > b.MaxCostUSD
	b.costUsedMu.Unlock()
	return exceeded
}

// String returns a human-readable summary.
func (b *Budget) String() string {
	return fmt.Sprintf("tokens=%d/%d cost=$%.4f/$%.2f files=%d/%d",
		b.tokensUsed.Load(), b.MaxTokens,
		b.CostUsed(), b.MaxCostUSD,
		b.filesWritten.Load(), b.MaxFilesWrite)
}

// BudgetManager aggregates budgets across all agents in a session.
type BudgetManager struct {
	session *Budget
	mu      sync.Mutex
	agents  map[string]*Budget
}

func NewBudgetManager(sessionBudget *Budget) *BudgetManager {
	return &BudgetManager{
		session: sessionBudget,
		agents:  make(map[string]*Budget),
	}
}

// AgentBudget returns or creates a budget for the given agent.
func (bm *BudgetManager) AgentBudget(agentID string) *Budget {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if b, ok := bm.agents[agentID]; ok {
		return b
	}

	// Agent inherits session limits (could be per-agent in future)
	b := NewBudget(bm.session.MaxTokens, bm.session.MaxCostUSD, bm.session.MaxFilesWrite)
	bm.agents[agentID] = b
	return b
}

// SessionBudget returns the session-wide budget.
func (bm *BudgetManager) SessionBudget() *Budget {
	return bm.session
}

// RecordUsage records tokens/cost against both the agent and session budgets.
func (bm *BudgetManager) RecordUsage(agentID string, inputTokens, outputTokens int) error {
	agentBudget := bm.AgentBudget(agentID)

	if err := agentBudget.RecordTokens(inputTokens, outputTokens); err != nil {
		return fmt.Errorf("agent %s: %w", agentID, err)
	}

	// Also record against session
	if err := bm.session.RecordTokens(inputTokens, outputTokens); err != nil {
		return fmt.Errorf("session: %w", err)
	}

	// Estimate cost (rough: $3/M input, $15/M output for Sonnet)
	cost := float64(inputTokens)*3.0/1_000_000 + float64(outputTokens)*15.0/1_000_000
	if err := agentBudget.RecordCost(cost); err != nil {
		return fmt.Errorf("agent %s: %w", agentID, err)
	}
	return bm.session.RecordCost(cost)
}
