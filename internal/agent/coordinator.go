package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// Coordinator manages shared resources across parallel agents.
// All agents share one coordinator — it prevents:
//   - Two agents editing the same file (PathLocker)
//   - Concurrent git operations (GitMutex)
//   - Interleaved terminal output (IOQueue)
//   - Provider rate limit violations (RateLimiter)
type Coordinator struct {
	pathLocker  *PathLocker
	gitSem      *semaphore.Weighted
	ioQueue     chan RenderMsg
	rateLimiter *ProviderRateLimiter
}

func NewCoordinator() *Coordinator {
	c := &Coordinator{
		pathLocker:  NewPathLocker(),
		gitSem:      semaphore.NewWeighted(1), // Exclusive: one git op at a time
		ioQueue:     make(chan RenderMsg, 256),
		rateLimiter: NewProviderRateLimiter(),
	}
	return c
}

// IOQueue returns the channel for serialized terminal output.
// A single goroutine should consume this and write to the terminal.
func (c *Coordinator) IOQueue() <-chan RenderMsg {
	return c.ioQueue
}

// Send queues a message for terminal rendering. Non-blocking with timeout.
func (c *Coordinator) Send(msg RenderMsg) {
	select {
	case c.ioQueue <- msg:
	case <-time.After(100 * time.Millisecond):
		// Drop message if queue is full — prevents deadlock
	}
}

// AcquireGit acquires exclusive access to git operations.
func (c *Coordinator) AcquireGit(ctx context.Context) error {
	return c.gitSem.Acquire(ctx, 1)
}

// ReleaseGit releases git access.
func (c *Coordinator) ReleaseGit() {
	c.gitSem.Release(1)
}

// LockFileRead acquires a read lock on a file path.
// Multiple agents can read the same file concurrently.
func (c *Coordinator) LockFileRead(path string) {
	c.pathLocker.RLock(path)
}

// UnlockFileRead releases a file read lock.
func (c *Coordinator) UnlockFileRead(path string) {
	c.pathLocker.RUnlock(path)
}

// LockFileWrite acquires an exclusive write lock on a file path.
// Only one agent can write to a given file at a time.
func (c *Coordinator) LockFileWrite(path string) {
	c.pathLocker.Lock(path)
}

// UnlockFileWrite releases a file write lock.
func (c *Coordinator) UnlockFileWrite(path string) {
	c.pathLocker.Unlock(path)
}

// WaitForRate waits for the rate limiter to allow a request to the given provider.
func (c *Coordinator) WaitForRate(ctx context.Context, providerName string) error {
	return c.rateLimiter.Wait(ctx, providerName)
}

// Close shuts down the coordinator.
func (c *Coordinator) Close() {
	close(c.ioQueue)
}

// RenderMsg is a message destined for terminal output.
type RenderMsg struct {
	AgentID string
	Type    RenderMsgType
	Content string
}

type RenderMsgType int

const (
	RenderText     RenderMsgType = iota // Streaming text chunk
	RenderToolCall                      // Tool call notification
	RenderToolDone                      // Tool result notification
	RenderStatus                        // Agent status update
	RenderError                         // Error message
	RenderDone                          // Agent finished
)

// PathLocker provides per-path read/write locks.
// Uses sync.Map for lock-free path lookup + sync.RWMutex per path.
// Multiple agents CAN read the same file concurrently (RLock).
// Only one agent can write a given file at a time (Lock).
type PathLocker struct {
	locks sync.Map // map[string]*sync.RWMutex
}

func NewPathLocker() *PathLocker {
	return &PathLocker{}
}

func (pl *PathLocker) getMutex(path string) *sync.RWMutex {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	actual, _ := pl.locks.LoadOrStore(abs, &sync.RWMutex{})
	return actual.(*sync.RWMutex)
}

func (pl *PathLocker) RLock(path string)   { pl.getMutex(path).RLock() }
func (pl *PathLocker) RUnlock(path string) { pl.getMutex(path).RUnlock() }
func (pl *PathLocker) Lock(path string)    { pl.getMutex(path).Lock() }
func (pl *PathLocker) Unlock(path string)  { pl.getMutex(path).Unlock() }

// ProviderRateLimiter enforces per-provider request rate limits.
type ProviderRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func NewProviderRateLimiter() *ProviderRateLimiter {
	return &ProviderRateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// Wait blocks until the rate limiter allows a request.
func (prl *ProviderRateLimiter) Wait(ctx context.Context, provider string) error {
	prl.mu.Lock()
	limiter, ok := prl.limiters[provider]
	if !ok {
		// Default: 50 requests/minute with burst of 10
		limiter = rate.NewLimiter(rate.Every(time.Second*60/50), 10)
		prl.limiters[provider] = limiter
	}
	prl.mu.Unlock()

	return limiter.Wait(ctx)
}

// SetRate configures the rate limit for a specific provider.
func (prl *ProviderRateLimiter) SetRate(provider string, requestsPerMinute int, burst int) {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if requestsPerMinute <= 0 {
		requestsPerMinute = 50
	}
	if burst <= 0 {
		burst = 10
	}

	prl.limiters[provider] = rate.NewLimiter(
		rate.Every(time.Duration(60/requestsPerMinute)*time.Second),
		burst,
	)
	fmt.Printf("  rate limiter: %s → %d req/min, burst %d\n", provider, requestsPerMinute, burst)
}
