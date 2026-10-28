package agents_test

import (
	"fmt"
	"testing"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/llm"
)

func TestSlidingMemory_CapAndPrune(t *testing.T) {
	mem := agents.NewSlidingMemory(15)

	// Add 25 messages
	for i := 1; i <= 25; i++ {
		mem.AddMessage(llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		})
	}

	if mem.Count() != 15 {
		t.Fatalf("expected 15 messages, got %d", mem.Count())
	}

	msgs := mem.GetMessages()
	if len(msgs) != 15 {
		t.Fatalf("expected 15 messages, got %d", len(msgs))
	}

	// First message should be Message 11 (messages 1..10 pruned)
	if msgs[0].Content != "Message 11" {
		t.Errorf("expected oldest message to be 'Message 11', got '%s'", msgs[0].Content)
	}

	// Last message should be Message 25
	if msgs[14].Content != "Message 25" {
		t.Errorf("expected newest message to be 'Message 25', got '%s'", msgs[14].Content)
	}
}

func TestSlidingMemory_SetMessagesPruning(t *testing.T) {
	mem := agents.NewSlidingMemory(5)

	var initial []llm.Message
	for i := 1; i <= 10; i++ {
		initial = append(initial, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("Turn %d", i),
		})
	}

	mem.SetMessages(initial)

	if mem.Count() != 5 {
		t.Fatalf("expected 5 messages, got %d", mem.Count())
	}

	msgs := mem.GetMessages()
	if msgs[0].Content != "Turn 6" || msgs[4].Content != "Turn 10" {
		t.Errorf("unexpected pruned messages: %v", msgs)
	}
}

