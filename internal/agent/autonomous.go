package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TaskRequest is a work request that one agent sends to another.
type TaskRequest struct {
	ID          string
	From        string         // Requesting agent ID
	Task        string         // Task description
	Priority    TaskPriority
	WaitMode    WaitMode       // Block or async
	ResultCh    chan TaskResult // Channel for the result
	CreatedAt   time.Time
}

type TaskPriority int

const (
	PriorityNormal TaskPriority = iota
	PriorityHigh
	PriorityCritical
)

// WaitMode controls how the requesting agent waits for results.
type WaitMode int

const (
	WaitBlock WaitMode = iota // Requesting agent blocks until result arrives
	WaitAsync                 // Requesting agent continues; fetches result later
)

// TaskResult is the output from a delegated task.
type TaskResult struct {
	RequestID string
	AgentID   string
	Output    string
	Error     error
	Duration  time.Duration
}

// Orchestrator manages autonomous agent workflows.
// Agents can:
//   - Spawn sub-agents for parallel work
//   - Delegate tasks to other agents (sync or async)
//   - Wait for results or continue and poll later
//   - Share findings via the message bus
type Orchestrator struct {
	pool       *Pool
	bus        *MessageBus

	mu         sync.Mutex
	pending    map[string]*TaskRequest // request ID → pending task
	completed  map[string]*TaskResult  // request ID → completed result
	nextReqID  int
}

func NewOrchestrator(pool *Pool, bus *MessageBus) *Orchestrator {
	return &Orchestrator{
		pool:      pool,
		bus:       bus,
		pending:   make(map[string]*TaskRequest),
		completed: make(map[string]*TaskResult),
	}
}

// Delegate sends a task to a new sub-agent.
// With WaitBlock: blocks until the sub-agent completes and returns the result.
// With WaitAsync: returns immediately with a request ID; call FetchResult later.
func (o *Orchestrator) Delegate(ctx context.Context, from, task string, priority TaskPriority, wait WaitMode) (string, *TaskResult, error) {
	reqID := o.nextRequestID()

	resultCh := make(chan TaskResult, 1)

	req := &TaskRequest{
		ID:        reqID,
		From:      from,
		Task:      task,
		Priority:  priority,
		WaitMode:  wait,
		ResultCh:  resultCh,
		CreatedAt: time.Now(),
	}

	o.mu.Lock()
	o.pending[reqID] = req
	o.mu.Unlock()

	// Notify via message bus
	o.bus.Publish(AgentMessage{
		From:    from,
		Topic:   "task-delegation",
		Type:    MsgHandoff,
		Content: fmt.Sprintf("delegating task %s: %s", reqID, task),
	})

	// Spawn sub-agent
	agentID, events, err := o.pool.Spawn(ctx, task)
	if err != nil {
		o.mu.Lock()
		delete(o.pending, reqID)
		o.mu.Unlock()
		return reqID, nil, fmt.Errorf("spawn sub-agent: %w", err)
	}

	// Collect results in background
	go o.collectResult(reqID, agentID, events, resultCh)

	if wait == WaitBlock {
		// Block until result arrives
		select {
		case result := <-resultCh:
			return reqID, &result, result.Error
		case <-ctx.Done():
			o.pool.Cancel(agentID)
			return reqID, nil, ctx.Err()
		}
	}

	// WaitAsync: return immediately
	return reqID, nil, nil
}

// FetchResult retrieves the result of an async-delegated task.
// Returns nil if the task hasn't completed yet.
func (o *Orchestrator) FetchResult(requestID string) *TaskResult {
	o.mu.Lock()
	defer o.mu.Unlock()

	if result, ok := o.completed[requestID]; ok {
		return result
	}
	return nil
}

// WaitForResult blocks until an async task completes.
func (o *Orchestrator) WaitForResult(ctx context.Context, requestID string) (*TaskResult, error) {
	o.mu.Lock()
	req, ok := o.pending[requestID]
	if !ok {
		// Check if already completed
		if result, done := o.completed[requestID]; done {
			o.mu.Unlock()
			return result, nil
		}
		o.mu.Unlock()
		return nil, fmt.Errorf("request %s not found", requestID)
	}
	o.mu.Unlock()

	select {
	case result := <-req.ResultCh:
		return &result, result.Error
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PendingTasks returns the number of in-flight delegated tasks.
func (o *Orchestrator) PendingTasks() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.pending)
}

// collectResult watches a sub-agent's events and collects its output.
func (o *Orchestrator) collectResult(reqID, agentID string, events <-chan AgentEvent, resultCh chan<- TaskResult) {
	start := time.Now()
	var output string

	for event := range events {
		switch event.Type {
		case AgentEventText:
			output += event.Text

		case AgentEventDone:
			result := TaskResult{
				RequestID: reqID,
				AgentID:   agentID,
				Output:    output,
				Duration:  time.Since(start),
			}

			// Store result
			o.mu.Lock()
			delete(o.pending, reqID)
			o.completed[reqID] = &result
			o.mu.Unlock()

			// Notify via bus
			o.bus.Publish(AgentMessage{
				From:    agentID,
				Topic:   "task-completed",
				Type:    MsgFinding,
				Content: fmt.Sprintf("task %s completed in %s: %d chars output", reqID, result.Duration.Round(time.Millisecond), len(output)),
			})

			// Send to waiting caller
			select {
			case resultCh <- result:
			default:
			}
			return

		case AgentEventError:
			result := TaskResult{
				RequestID: reqID,
				AgentID:   agentID,
				Output:    output,
				Error:     event.Error,
				Duration:  time.Since(start),
			}

			o.mu.Lock()
			delete(o.pending, reqID)
			o.completed[reqID] = &result
			o.mu.Unlock()

			select {
			case resultCh <- result:
			default:
			}
			return
		}
	}
}

func (o *Orchestrator) nextRequestID() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextReqID++
	return fmt.Sprintf("req-%d", o.nextReqID)
}
