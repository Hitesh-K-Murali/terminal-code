package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"github.com/Hitesh-K-Murali/terminal-code/internal/engine"
)

// Pool manages a bounded set of concurrent agents.
// Each agent runs in its own goroutine with its own conversation history,
// but shares the Coordinator for resource access.
type Pool struct {
	maxAgents   int
	sem         *semaphore.Weighted
	coordinator *Coordinator
	budgetMgr   *BudgetManager
	engineFn    func() *engine.Engine // Factory: creates engine per agent

	mu      sync.Mutex
	agents  map[string]*runningAgent
	nextID  atomic.Int64
}

type runningAgent struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
	result *AgentResult
}

// AgentEvent is emitted by a running agent for UI updates.
type AgentEvent struct {
	AgentID string
	Type    AgentEventType
	Text    string
	Error   error
}

type AgentEventType int

const (
	AgentEventText   AgentEventType = iota // Streaming text
	AgentEventTool                         // Tool call/result
	AgentEventDone                         // Agent finished
	AgentEventError                        // Error
)

// AgentResult is the final output of an agent's work.
type AgentResult struct {
	AgentID      string
	Output       string
	TokensUsed   int64
	CostUSD      float64
	FilesWritten int32
	Error        error
}

// NewPool creates an agent pool with bounded concurrency.
func NewPool(maxAgents int, coordinator *Coordinator, budgetMgr *BudgetManager, engineFn func() *engine.Engine) *Pool {
	if maxAgents <= 0 {
		maxAgents = 3
	}
	return &Pool{
		maxAgents:   maxAgents,
		sem:         semaphore.NewWeighted(int64(maxAgents)),
		coordinator: coordinator,
		budgetMgr:   budgetMgr,
		engineFn:    engineFn,
		agents:      make(map[string]*runningAgent),
	}
}

// Spawn starts a new agent to work on the given task.
// Returns immediately; the agent runs in a background goroutine.
// Events are delivered via the returned channel.
func (p *Pool) Spawn(ctx context.Context, task string) (string, <-chan AgentEvent, error) {
	id := fmt.Sprintf("agent-%d", p.nextID.Add(1))

	// Try to acquire a slot (non-blocking check first)
	if !p.sem.TryAcquire(1) {
		return "", nil, fmt.Errorf("agent pool full (%d/%d). Wait for an agent to finish or increase max_parallel_agents", p.ActiveCount(), p.maxAgents)
	}

	ctx, cancel := context.WithCancel(ctx)
	events := make(chan AgentEvent, 64)

	ra := &runningAgent{
		id:     id,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	p.mu.Lock()
	p.agents[id] = ra
	p.mu.Unlock()

	go func() {
		defer func() {
			p.sem.Release(1)
			close(events)
			close(ra.done)

			p.mu.Lock()
			delete(p.agents, id)
			p.mu.Unlock()
		}()

		result := p.runAgent(ctx, id, task, events)
		ra.result = result

		if result.Error != nil {
			events <- AgentEvent{
				AgentID: id,
				Type:    AgentEventError,
				Error:   result.Error,
			}
		}

		events <- AgentEvent{
			AgentID: id,
			Type:    AgentEventDone,
			Text:    fmt.Sprintf("finished (%s)", p.budgetMgr.AgentBudget(id).String()),
		}
	}()

	return id, events, nil
}

// runAgent executes a task in an agent's isolated context.
func (p *Pool) runAgent(ctx context.Context, agentID, task string, events chan<- AgentEvent) *AgentResult {
	budget := p.budgetMgr.AgentBudget(agentID)
	eng := p.engineFn()

	result := &AgentResult{AgentID: agentID}

	// Stream the agent's work
	ch, err := eng.Send(ctx, task)
	if err != nil {
		result.Error = err
		return result
	}

	var output string

	for event := range ch {
		// Check budget before processing
		if budget.Exceeded() {
			result.Error = fmt.Errorf("budget exceeded: %s", budget.String())
			return result
		}

		switch event.Type {
		case 0: // EventText
			output += event.Text
			events <- AgentEvent{
				AgentID: agentID,
				Type:    AgentEventText,
				Text:    event.Text,
			}

		case 1: // EventToolUse
			if event.ToolCall != nil {
				events <- AgentEvent{
					AgentID: agentID,
					Type:    AgentEventTool,
					Text:    fmt.Sprintf("calling %s", event.ToolCall.Name),
				}
			}

		case 3: // EventDone
			if event.Usage != nil {
				if err := p.budgetMgr.RecordUsage(agentID, event.Usage.InputTokens, event.Usage.OutputTokens); err != nil {
					log.Printf("agent %s: budget warning: %v", agentID, err)
				}
			}

		case 4: // EventError
			result.Error = event.Error
			return result
		}
	}

	result.Output = output
	result.TokensUsed = budget.TokensUsed()
	result.CostUSD = budget.CostUsed()
	result.FilesWritten = budget.FilesWritten()
	return result
}

// Cancel stops a running agent.
func (p *Pool) Cancel(agentID string) error {
	p.mu.Lock()
	ra, ok := p.agents[agentID]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	ra.cancel()
	<-ra.done
	return nil
}

// CancelAll stops all running agents.
func (p *Pool) CancelAll() {
	p.mu.Lock()
	agents := make([]*runningAgent, 0, len(p.agents))
	for _, ra := range p.agents {
		agents = append(agents, ra)
	}
	p.mu.Unlock()

	for _, ra := range agents {
		ra.cancel()
	}
	for _, ra := range agents {
		<-ra.done
	}
}

// ActiveCount returns the number of running agents.
func (p *Pool) ActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.agents)
}

// MaxAgents returns the pool capacity.
func (p *Pool) MaxAgents() int {
	return p.maxAgents
}
