package inbound

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
)

// EventBroadcaster manages agent topic subscriptions and real-time SSE event publishing.
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan *InboundEvent][]string
}

// NewEventBroadcaster creates a new EventBroadcaster instance.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		subscribers: make(map[chan *InboundEvent][]string),
	}
}

// Subscribe registers a new subscriber channel listening to the provided topic patterns (e.g. "slack:*", "*").
func (b *EventBroadcaster) Subscribe(topics []string) <-chan *InboundEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(topics) == 0 {
		topics = []string{"*"}
	}

	// Buffer 100 events to prevent slow subscribers from blocking the broadcaster
	ch := make(chan *InboundEvent, 100)
	b.subscribers[ch] = topics
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (b *EventBroadcaster) Unsubscribe(subscriber <-chan *InboundEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subscribers {
		if (<-chan *InboundEvent)(ch) == subscriber {
			delete(b.subscribers, ch)
			close(ch)
			break
		}
	}
}

func matchesTopic(pattern, topic string) bool {
	if pattern == "*" || pattern == topic {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(topic, prefix) {
			return true
		}
	}
	matched, _ := path.Match(pattern, topic)
	return matched
}

// Publish dispatches an event to all matching subscribers. Returns the number of recipients.
func (b *EventBroadcaster) Publish(event *InboundEvent) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	delivered := 0
	for ch, patterns := range b.subscribers {
		matched := false
		for _, pattern := range patterns {
			if matchesTopic(pattern, event.Topic) {
				matched = true
				break
			}
		}

		if matched {
			select {
			case ch <- event:
				delivered++
			default:
				// Channel buffer full; drop to protect broadcaster responsiveness
			}
		}
	}

	return delivered
}

// FormatSSEMessage formats an InboundEvent as a standard Server-Sent Events (SSE) message block.
func (b *EventBroadcaster) FormatSSEMessage(event *InboundEvent) string {
	dataBytes, _ := json.Marshal(event)
	return fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", event.EventID, event.Topic, string(dataBytes))
}
