package agents

import (
	"sync"

	"clawbridge/pkg/llm"
)

// DefaultMaxMemoryTurns specifies the default sliding window capacity (15 turns).
const DefaultMaxMemoryTurns = 15

// SlidingMemory maintains an in-memory sliding window of the last N conversation turns.
type SlidingMemory struct {
	mu       sync.RWMutex
	maxTurns int
	messages []llm.Message
}

// NewSlidingMemory creates a new SlidingMemory with the specified turn limit.
func NewSlidingMemory(maxTurns int) *SlidingMemory {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxMemoryTurns
	}
	return &SlidingMemory{
		maxTurns: maxTurns,
		messages: make([]llm.Message, 0, maxTurns),
	}
}

// AddMessage appends a single message and prunes older messages beyond the limit.
func (m *SlidingMemory) AddMessage(msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, msg)
	if len(m.messages) > m.maxTurns {
		m.messages = m.messages[len(m.messages)-m.maxTurns:]
	}
}

// AddMessages appends multiple messages and prunes accordingly.
func (m *SlidingMemory) AddMessages(msgs []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, msgs...)
	if len(m.messages) > m.maxTurns {
		m.messages = m.messages[len(m.messages)-m.maxTurns:]
	}
}

// SetMessages overwrites current memory with the provided messages and prunes if needed.
func (m *SlidingMemory) SetMessages(msgs []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(msgs) > m.maxTurns {
		m.messages = make([]llm.Message, m.maxTurns)
		copy(m.messages, msgs[len(msgs)-m.maxTurns:])
	} else {
		m.messages = make([]llm.Message, len(msgs))
		copy(m.messages, msgs)
	}
}

// GetMessages returns a copy of all retained conversation messages.
func (m *SlidingMemory) GetMessages() []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	copied := make([]llm.Message, len(m.messages))
	copy(copied, m.messages)
	return copied
}

// Count returns the current number of messages stored.
func (m *SlidingMemory) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// Clear resets the memory.
func (m *SlidingMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]llm.Message, 0, m.maxTurns)
}
