package agents_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/llm"
	"clawbridge/pkg/vault"
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

func TestTieredMemory_CompactionAndPersistence(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "memory_test.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create database manager: %v", err)
	}
	defer dbMgr.Close()

	if err := dbMgr.Initialize(); err != nil {
		t.Fatalf("failed to initialize db schema: %v", err)
	}

	agentName := "nemoclaw"
	mem := agents.NewTieredMemory(agentName, 5, dbMgr)

	// Add 12 turns (5 kept in RAM, 7 compacted to summary)
	for i := 1; i <= 12; i++ {
		mem.AddMessage(llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("Goal number %d to achieve", i),
		})
		mem.AddMessage(llm.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Plan executed for goal %d", i),
		})
	}

	// RAM should strictly hold exactly 5 messages
	if mem.Count() != 5 {
		t.Errorf("expected 5 messages in RAM, got %d", mem.Count())
	}

	// Total turns tracked should be 24
	if mem.GetTotalTurns() != 24 {
		t.Errorf("expected total turns 24, got %d", mem.GetTotalTurns())
	}

	// Summary should not be empty
	summary := mem.GetSummary()
	if summary == "" {
		t.Fatal("expected summary to contain compacted information, got empty string")
	}

	if !strings.Contains(summary, "Goal number 1") {
		t.Errorf("expected summary to mention earliest goals, got: %s", summary)
	}

	// Verify Context Messages include historical system summary
	contextMsgs := mem.GetContextMessages()
	if len(contextMsgs) != 6 { // 1 system summary + 5 active messages
		t.Fatalf("expected 6 context messages, got %d", len(contextMsgs))
	}
	if contextMsgs[0].Role != "system" || !strings.Contains(contextMsgs[0].Content, "[Historical Context Summary") {
		t.Errorf("expected first context message to be system summary, got: %+v", contextMsgs[0])
	}

	// Test persistence across restarts: open new TieredMemory with same agent name and db
	reloadedMem := agents.NewTieredMemory(agentName, 5, dbMgr)
	if reloadedMem.GetSummary() != summary {
		t.Errorf("expected reloaded memory summary to match persisted summary.\nExpected:\n%s\nGot:\n%s", summary, reloadedMem.GetSummary())
	}
	if reloadedMem.GetTotalTurns() != 24 {
		t.Errorf("expected reloaded memory total turns to be 24, got %d", reloadedMem.GetTotalTurns())
	}
}
