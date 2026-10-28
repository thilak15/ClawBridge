package agents

import (
	"fmt"
	"strings"
	"sync"

	"clawbridge/pkg/llm"
)

// DefaultMaxMemoryTurns specifies the default sliding window capacity (15 turns).
const DefaultMaxMemoryTurns = 15

// MaxSummaryChars caps the length of the compacted summary to prevent prompt bloat.
const MaxSummaryChars = 2500

// MemoryPersistence provides an abstraction for persisting and retrieving rolling summaries.
type MemoryPersistence interface {
	SaveAgentMemory(agentName, summary string, turnCount int) error
	GetAgentMemory(agentName string) (string, int, error)
}

// SlidingMemory maintains an in-memory sliding window of the last N conversation turns
// with optional tiered SQLite WAL persistence and rolling summary compaction.
type SlidingMemory struct {
	mu          sync.RWMutex
	maxTurns    int
	messages    []llm.Message
	agentName   string
	summary     string
	totalTurns  int
	store       MemoryPersistence
	compactFunc func(existingSummary string, evicted []llm.Message) string
}

// NewSlidingMemory creates a new in-memory SlidingMemory with the specified turn limit.
func NewSlidingMemory(maxTurns int) *SlidingMemory {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxMemoryTurns
	}
	return &SlidingMemory{
		maxTurns: maxTurns,
		messages: make([]llm.Message, 0, maxTurns),
	}
}

// NewTieredMemory creates a new SlidingMemory with SQLite WAL persistence and automatic compaction.
func NewTieredMemory(agentName string, maxTurns int, store MemoryPersistence) *SlidingMemory {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxMemoryTurns
	}
	m := &SlidingMemory{
		agentName: agentName,
		maxTurns:  maxTurns,
		messages:  make([]llm.Message, 0, maxTurns),
		store:     store,
	}

	if store != nil && agentName != "" {
		if summ, turns, err := store.GetAgentMemory(agentName); err == nil && summ != "" {
			m.summary = summ
			m.totalTurns = turns
		}
	}

	return m
}

// SetCompactor overrides the default compaction function.
func (m *SlidingMemory) SetCompactor(fn func(existingSummary string, evicted []llm.Message) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compactFunc = fn
}

// AddMessage appends a single message, updates turn count, and prunes older messages beyond the limit.
func (m *SlidingMemory) AddMessage(msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalTurns++
	m.messages = append(m.messages, msg)
	m.pruneAndCompactLocked()
}

// AddMessages appends multiple messages and prunes accordingly.
func (m *SlidingMemory) AddMessages(msgs []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalTurns += len(msgs)
	m.messages = append(m.messages, msgs...)
	m.pruneAndCompactLocked()
}

// SetMessages overwrites current memory with the provided messages and prunes if needed.
func (m *SlidingMemory) SetMessages(msgs []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]llm.Message, len(msgs))
	copy(m.messages, msgs)
	m.totalTurns = len(msgs)
	m.pruneAndCompactLocked()
}

// pruneAndCompactLocked prunes messages exceeding maxTurns and compacts them into the rolling summary.
func (m *SlidingMemory) pruneAndCompactLocked() {
	if len(m.messages) <= m.maxTurns {
		return
	}

	evictedCount := len(m.messages) - m.maxTurns
	evicted := make([]llm.Message, evictedCount)
	copy(evicted, m.messages[:evictedCount])
	m.messages = m.messages[evictedCount:]

	if m.compactFunc != nil {
		m.summary = m.compactFunc(m.summary, evicted)
	} else {
		m.summary = DefaultCompactSummary(m.summary, evicted)
	}

	if m.store != nil && m.agentName != "" {
		_ = m.store.SaveAgentMemory(m.agentName, m.summary, m.totalTurns)
	}
}

// DefaultCompactSummary generates a concise bulleted summary of evicted messages.
func DefaultCompactSummary(existingSummary string, evicted []llm.Message) string {
	var sb strings.Builder
	if existingSummary != "" {
		sb.WriteString(existingSummary)
		if !strings.HasSuffix(existingSummary, "\n") {
			sb.WriteString("\n")
		}
	}

	for _, msg := range evicted {
		content := strings.TrimSpace(msg.Content)
		if len(content) > 120 {
			content = content[:117] + "..."
		}
		switch msg.Role {
		case "user":
			if content != "" {
				sb.WriteString(fmt.Sprintf("- User instruction: %s\n", content))
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, tc.Name)
				}
				sb.WriteString(fmt.Sprintf("- Invoked tools: %s\n", strings.Join(calls, ", ")))
			} else if content != "" {
				sb.WriteString(fmt.Sprintf("- Assistant response: %s\n", content))
			}
		case "tool":
			if content != "" {
				sb.WriteString(fmt.Sprintf("- Tool outcome: %s\n", content))
			}
		}
	}

	result := sb.String()
	if len(result) > MaxSummaryChars {
		// Truncate from the beginning, keeping the most recent context
		result = "... " + result[len(result)-MaxSummaryChars+4:]
	}
	return result
}

// GetMessages returns a copy of all active conversation messages retained in RAM.
func (m *SlidingMemory) GetMessages() []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	copied := make([]llm.Message, len(m.messages))
	copy(copied, m.messages)
	return copied
}

// GetContextMessages returns active messages prepended with a system summary message if a summary exists.
func (m *SlidingMemory) GetContextMessages() []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	raw := make([]llm.Message, len(m.messages))
	copy(raw, m.messages)

	if m.summary == "" {
		return raw
	}

	summaryMsg := llm.Message{
		Role: "system",
		Content: fmt.Sprintf(
			"[Historical Context Summary - prior %d turns]:\n%s",
			m.totalTurns-len(m.messages),
			m.summary,
		),
	}

	result := make([]llm.Message, 0, len(raw)+1)
	result = append(result, summaryMsg)
	result = append(result, raw...)
	return result
}

// GetSummary returns the current compacted rolling summary.
func (m *SlidingMemory) GetSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.summary
}

// SetSummary manually updates the rolling summary and updates the store if available.
func (m *SlidingMemory) SetSummary(summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summary = summary
	if m.store != nil && m.agentName != "" {
		_ = m.store.SaveAgentMemory(m.agentName, m.summary, m.totalTurns)
	}
}

// GetTotalTurns returns the cumulative count of turns processed.
func (m *SlidingMemory) GetTotalTurns() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalTurns
}

// Count returns the current number of active messages stored in RAM.
func (m *SlidingMemory) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// Clear resets the memory and clears the summary.
func (m *SlidingMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]llm.Message, 0, m.maxTurns)
	m.summary = ""
	m.totalTurns = 0
	if m.store != nil && m.agentName != "" {
		_ = m.store.SaveAgentMemory(m.agentName, "", 0)
	}
}
