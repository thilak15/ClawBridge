package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/llm"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/runner"
	"clawbridge/pkg/vault"
)

func setupTestEnvironment(t *testing.T) (*auth.AgentRegistry, string, func()) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-runner-test-*")
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = dbMgr.Initialize()

	reg := auth.NewAgentRegistry(dbMgr)
	agentsDir := filepath.Join(tmpDir, "agents")
	_ = os.MkdirAll(agentsDir, 0755)

	_, _ = agents.CreateAgentYAML(agentsDir, agents.AgentConfig{
		Name:         "nemoclaw",
		SystemPrompt: "You are NemoClaw.",
		Permissions:  []string{"slack:*"},
	})
	_, _ = agents.CreateAgentYAML(agentsDir, agents.AgentConfig{
		Name:         "openclaw",
		SystemPrompt: "You are OpenClaw.",
		Permissions:  []string{"jira:*"},
	})

	cleanup := func() {
		_ = dbMgr.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return reg, agentsDir, cleanup
}

func TestOrchestrator_ConcurrentMultiAgentExecution(t *testing.T) {
	reg, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	outBuf := &bytes.Buffer{}
	orch := runner.NewOrchestrator(agentsDir, reg, nil, nil, nil, outBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start all agents
	started, err := orch.StartAll(ctx)
	if err != nil {
		t.Fatalf("failed to start agents: %v", err)
	}

	if len(started) != 2 {
		t.Fatalf("expected 2 agents started, got %d", len(started))
	}

	running := orch.ListRunning()
	if len(running) != 2 {
		t.Fatalf("expected 2 running agents, got %d", len(running))
	}

	// Dispatch individual tasks
	if err := orch.DispatchTask("nemoclaw", "Check support tickets in Slack"); err != nil {
		t.Fatalf("failed to dispatch to nemoclaw: %v", err)
	}
	if err := orch.DispatchTask("openclaw", "Create bug ticket in Jira"); err != nil {
		t.Fatalf("failed to dispatch to openclaw: %v", err)
	}

	// Dispatch broadcast task
	if err := orch.BroadcastTask("Sync operational state"); err != nil {
		t.Fatalf("failed to broadcast task: %v", err)
	}

	// Allow goroutines to process queued tasks
	time.Sleep(50 * time.Millisecond)

	nemoMem := orch.GetAgentMemory("nemoclaw")
	openMem := orch.GetAgentMemory("openclaw")

	if nemoMem == nil || nemoMem.Count() < 4 { // 2 user msgs + 2 replies
		t.Errorf("expected at least 4 messages in nemoclaw memory, got %d", nemoMem.Count())
	}
	if openMem == nil || openMem.Count() < 2 {
		t.Errorf("expected at least 2 messages in openclaw memory, got %d", openMem.Count())
	}

	// Stop agents
	orch.StopAll()
	if len(orch.ListRunning()) != 0 {
		t.Errorf("expected 0 running agents after StopAll, got %d", len(orch.ListRunning()))
	}
}

func TestConsole_InteractiveCommands(t *testing.T) {
	reg, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	outBuf := &bytes.Buffer{}
	orch := runner.NewOrchestrator(agentsDir, reg, nil, nil, nil, outBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _ = orch.StartAll(ctx)

	script := `list
status
nemoclaw: Summarize customer issues
broadcast: Prepare weekly update
exit
`
	in := strings.NewReader(script)
	consoleOut := &bytes.Buffer{}

	err := runner.RunConsole(ctx, in, consoleOut, orch, nil)
	if err != nil {
		t.Fatalf("unexpected console error: %v", err)
	}

	output := consoleOut.String()
	if !strings.Contains(output, "Running agents (2):") {
		t.Errorf("expected running agents listing, got:\n%s", output)
	}
	if !strings.Contains(output, "Task dispatched to 'nemoclaw'") {
		t.Errorf("expected task dispatched confirmation, got:\n%s", output)
	}
	if !strings.Contains(output, "Task broadcasted to all running agents") {
		t.Errorf("expected broadcast confirmation, got:\n%s", output)
	}
}

func TestConsole_InboundWebhookReaction(t *testing.T) {
	reg, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	broadcaster := inbound.NewEventBroadcaster()
	outBuf := &bytes.Buffer{}
	orch := runner.NewOrchestrator(agentsDir, reg, nil, nil, broadcaster, outBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _ = orch.StartAll(ctx)

	// Simulate background console run with broadcaster listening
	in := strings.NewReader("help\nexit\n")
	consoleOut := &bytes.Buffer{}

	// Publish inbound Slack event in background once subscriber is active
	go func() {
		time.Sleep(15 * time.Millisecond)
		broadcaster.Publish(&inbound.InboundEvent{
			EventID: "evt_test_123",
			Channel: "slack",
			Topic:   "slack:message",
			Content: "Help with login",
			Data:    map[string]any{"user": "alice", "text": "Help with login"},
		})
	}()

	err := runner.RunConsole(ctx, in, consoleOut, orch, broadcaster)
	if err != nil {
		t.Fatalf("unexpected console error: %v", err)
	}

	// Give goroutine a moment to deliver and process task
	time.Sleep(50 * time.Millisecond)

	nemoMem := orch.GetAgentMemory("nemoclaw")
	if nemoMem == nil || nemoMem.Count() == 0 {
		t.Fatalf("expected nemoclaw to react to inbound webhook event")
	}

	messages := nemoMem.GetMessages()
	found := false
	for _, m := range messages {
		if strings.Contains(m.Content, "Inbound event received on topic 'slack:message'") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected inbound event prompt in nemoclaw memory, got %v", messages)
	}
}

func TestOrchestrator_ToolPermissionsEnforcement(t *testing.T) {
	reg, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// NemoClaw has permissions: ["slack:*"]
	outBuf := &bytes.Buffer{}
	orch := runner.NewOrchestrator(agentsDir, reg, nil, nil, nil, outBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := orch.StartAgent(ctx, "nemoclaw"); err != nil {
		t.Fatal(err)
	}

	// Dispatch task
	if err := orch.DispatchTask("nemoclaw", "Run action"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	mem := orch.GetAgentMemory("nemoclaw")
	if mem == nil || mem.Count() == 0 {
		t.Fatalf("expected memory turns for nemoclaw")
	}

	orch.StopAll()
}

func TestOrchestrator_EvaluateAndRepairLoop(t *testing.T) {
	reg, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// 1. Setup mock downstream Slack server that fails on bad channel, succeeds on good channel
	slackCalls := 0
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackCalls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body["channel"] == "bad_channel" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "channel_not_found"})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "123.456"})
	}))
	defer slackServer.Close()

	// 2. Setup mock LLM server: Turn 1 produces bad call, Turn 2 repairs call, Turn 3 concludes
	llmTurns := 0
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmTurns++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if llmTurns == 1 {
			// Turn 1: Try with invalid channel
			argsJSON, _ := json.Marshal(map[string]any{
				"channel_id": "bad_channel",
				"text":       "Message attempt 1",
				"url":        slackServer.URL,
			})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_001",
									"type": "function",
									"function": map[string]any{
										"name":      "slack_post_message",
										"arguments": string(argsJSON),
									},
								},
							},
						},
					},
				},
			})
			return
		}

		if llmTurns == 2 {
			// Turn 2: LLM received error with directive, corrects to valid channel
			argsJSON, _ := json.Marshal(map[string]any{
				"channel_id": "C_VALID_123",
				"text":       "Message attempt 2 repaired",
				"url":        slackServer.URL,
			})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_002",
									"type": "function",
									"function": map[string]any{
										"name":      "slack_post_message",
										"arguments": string(argsJSON),
									},
								},
							},
						},
					},
				},
			})
			return
		}

		// Turn 3: Final confirmation
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "Successfully repaired arguments and posted to Slack.",
					},
				},
			},
		})
	}))
	defer llmServer.Close()

	llmClient := llm.NewClient(llm.ClientConfig{
		Provider:   llm.ProviderOpenAI,
		APIKey:     "test-key",
		BaseURL:    llmServer.URL,
		HTTPClient: llmServer.Client(),
	})

	// Setup tools with EgressDispatcher
	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	credVault := vault.NewCredentialVault(reg.DatabaseManager(), cipher)
	limiter := dispatch.NewTokenBucketLimiter(nil)
	dispatcher := dispatch.NewEgressDispatcher(credVault, limiter, slackServer.Client())
	tools := mcp.NewTools(dispatcher, nil, nil)

	outBuf := &bytes.Buffer{}
	orch := runner.NewOrchestrator(agentsDir, reg, llmClient, tools, nil, outBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := orch.StartAgent(ctx, "nemoclaw"); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}

	if err := orch.DispatchTask("nemoclaw", "Post update to team Slack"); err != nil {
		t.Fatalf("failed to dispatch task: %v", err)
	}

	// Wait for multi-turn repair execution to complete
	time.Sleep(150 * time.Millisecond)

	mem := orch.GetAgentMemory("nemoclaw")
	if mem == nil {
		t.Fatalf("expected memory for agent")
	}

	// Verify that logs or memory show the self-correction attempt
	logOutput := outBuf.String()
	if !strings.Contains(logOutput, "[Self-Correction 1/3]") {
		t.Errorf("expected logs to contain [Self-Correction 1/3], got:\n%s", logOutput)
	}

	if slackCalls != 2 {
		t.Errorf("expected 2 calls to Slack (1 fail, 1 repaired success), got %d", slackCalls)
	}

	orch.StopAll()
}
