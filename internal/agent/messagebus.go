package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MessageBus enables inter-agent communication.
// Agents can publish findings, subscribe to topics, and request help.
// The orchestrator uses this to coordinate multi-agent workflows.
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan AgentMessage  // topic → subscriber channels
	history     []AgentMessage                  // All messages (for late-joining agents)
	maxHistory  int
}

// AgentMessage is a typed message exchanged between agents.
type AgentMessage struct {
	From      string           `json:"from"`
	To        string           `json:"to,omitempty"`       // Empty = broadcast
	Topic     string           `json:"topic"`
	Type      AgentMessageType `json:"type"`
	Content   string           `json:"content"`
	Timestamp time.Time        `json:"timestamp"`
}

type AgentMessageType int

const (
	MsgFinding    AgentMessageType = iota // Agent discovered something useful
	MsgRequest                            // Agent needs help from another
	MsgResponse                           // Response to a request
	MsgStatus                             // Agent status update
	MsgHandoff                            // Agent handing off work to another
)

func (t AgentMessageType) String() string {
	switch t {
	case MsgFinding:
		return "finding"
	case MsgRequest:
		return "request"
	case MsgResponse:
		return "response"
	case MsgStatus:
		return "status"
	case MsgHandoff:
		return "handoff"
	default:
		return "unknown"
	}
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		subscribers: make(map[string][]chan AgentMessage),
		maxHistory:  1000,
	}
}

// Publish sends a message to all subscribers of the given topic.
// Also stored in history for late-joining agents.
func (mb *MessageBus) Publish(msg AgentMessage) {
	msg.Timestamp = time.Now()

	mb.mu.Lock()
	// Store in history
	mb.history = append(mb.history, msg)
	if len(mb.history) > mb.maxHistory {
		mb.history = mb.history[len(mb.history)-mb.maxHistory:]
	}

	// Get subscribers for this topic
	subs := make([]chan AgentMessage, len(mb.subscribers[msg.Topic]))
	copy(subs, mb.subscribers[msg.Topic])

	// Also notify wildcard subscribers ("*")
	wildcardSubs := make([]chan AgentMessage, len(mb.subscribers["*"]))
	copy(wildcardSubs, mb.subscribers["*"])
	mb.mu.Unlock()

	// Send to topic subscribers (non-blocking)
	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
			// Drop if subscriber is slow — prevents deadlock
		}
	}

	// Send to wildcard subscribers
	for _, ch := range wildcardSubs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Subscribe returns a channel that receives messages for the given topic.
// Use "*" to receive all messages.
func (mb *MessageBus) Subscribe(topic string, bufSize int) <-chan AgentMessage {
	ch := make(chan AgentMessage, bufSize)

	mb.mu.Lock()
	mb.subscribers[topic] = append(mb.subscribers[topic], ch)
	mb.mu.Unlock()

	return ch
}

// Unsubscribe removes a channel from a topic's subscriber list.
func (mb *MessageBus) Unsubscribe(topic string, ch <-chan AgentMessage) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	subs := mb.subscribers[topic]
	for i, sub := range subs {
		// Compare channel identity
		if fmt.Sprintf("%p", sub) == fmt.Sprintf("%p", ch) {
			mb.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// History returns recent messages, optionally filtered by topic.
func (mb *MessageBus) History(topic string, limit int) []AgentMessage {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	if topic == "" || topic == "*" {
		start := len(mb.history) - limit
		if start < 0 {
			start = 0
		}
		result := make([]AgentMessage, len(mb.history[start:]))
		copy(result, mb.history[start:])
		return result
	}

	var filtered []AgentMessage
	for i := len(mb.history) - 1; i >= 0 && len(filtered) < limit; i-- {
		if mb.history[i].Topic == topic {
			filtered = append(filtered, mb.history[i])
		}
	}

	// Reverse to chronological order
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered
}

// RequestAndWait sends a request message and waits for a response.
// Used when one agent needs information from another.
func (mb *MessageBus) RequestAndWait(ctx context.Context, from, topic, content string, timeout time.Duration) (AgentMessage, error) {
	// Subscribe to responses
	responseCh := mb.Subscribe("response:"+from, 1)
	defer mb.Unsubscribe("response:"+from, responseCh)

	// Publish the request
	mb.Publish(AgentMessage{
		From:    from,
		Topic:   topic,
		Type:    MsgRequest,
		Content: content,
	})

	// Wait for response
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case msg := <-responseCh:
		return msg, nil
	case <-ctx.Done():
		return AgentMessage{}, fmt.Errorf("request timeout after %s", timeout)
	}
}

// Respond sends a response to a specific agent's request.
func (mb *MessageBus) Respond(from, to, content string) {
	mb.Publish(AgentMessage{
		From:    from,
		To:      to,
		Topic:   "response:" + to,
		Type:    MsgResponse,
		Content: content,
	})
}
