package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/llm"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/vault"
)

func TestClient_DetectProviderFromEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("OPENAI_API_KEY", "")

	provider, key, model := llm.DetectProviderFromEnv()
	if provider != llm.ProviderGemini {
		t.Fatalf("expected ProviderGemini, got %v", provider)
	}
	if key != "test-gemini-key" {
		t.Fatalf("expected 'test-gemini-key', got '%s'", key)
	}
	if model != "gemini-3.8-flash" {
		t.Fatalf("expected 'gemini-3.8-flash', got '%s'", model)
	}
}

func TestNormalizeGeminiModel(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"gemini-1.5-flash", "gemini-3.8-flash"},
		{"gemini-1.5-pro", "gemini-3.8-flash"},
		{"gemini-2.0-flash", "gemini-3.8-flash"},
		{"gemini-2.5-flash", "gemini-3.8-flash"},
		{"gemini-1.5-flash-lite", "gemini-3.5-flash-lite"},
		{"gemini-2.5-flash-lite", "gemini-3.5-flash-lite"},
		{"gemini-3.8-flash", "gemini-3.8-flash"},
		{"gpt-4o", "gemini-3.8-flash"},
		{"", "gemini-3.8-flash"},
	}

	for _, c := range cases {
		got := llm.NormalizeGeminiModel(c.input)
		if got != c.expected {
			t.Errorf("for input %q expected %q, got %q", c.input, c.expected, got)
		}
	}
}

func TestDetectProviderFromKey(t *testing.T) {
	cases := []struct {
		key      string
		expected llm.ProviderType
	}{
		{"AIzaSyD-12345", llm.ProviderGemini},
		{"AQ.Ab8RN1234567890", llm.ProviderGemini},
		{"sk-proj-abcde12345", llm.ProviderOpenAI},
		{"sk-123456789", llm.ProviderOpenAI},
		{"sk-ant-api03-12345", llm.ProviderAnthropic},
		{"random-token-xyz", ""},
	}

	for _, c := range cases {
		got := llm.DetectProviderFromKey(c.key)
		if got != c.expected {
			t.Errorf("for key %q expected %v, got %v", c.key, c.expected, got)
		}
	}
}

func TestClient_OpenAIChatCompletion(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-openai-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! I am ready to help.",
					},
				},
			},
		})
	}))
	defer mockServer.Close()

	client := llm.NewClient(llm.ClientConfig{
		Provider:   llm.ProviderOpenAI,
		APIKey:     "test-openai-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	})

	req := llm.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []llm.Message{
			{Role: "user", Content: "Hello assistant"},
		},
	}

	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error calling openai: %v", err)
	}

	if resp.Content != "Hello! I am ready to help." {
		t.Fatalf("expected response 'Hello! I am ready to help.', got '%s'", resp.Content)
	}
}

func TestClient_GeminiChatCompletion(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-gemini-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Gemini response successful."},
						},
					},
				},
			},
		})
	}))
	defer mockServer.Close()

	client := llm.NewClient(llm.ClientConfig{
		Provider:   llm.ProviderGemini,
		APIKey:     "test-gemini-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	})

	req := llm.ChatRequest{
		Model: "gemini-1.5-flash",
		Messages: []llm.Message{
			{Role: "user", Content: "Test message"},
		},
	}

	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error calling gemini: %v", err)
	}

	if resp.Content != "Gemini response successful." {
		t.Fatalf("expected 'Gemini response successful.', got '%s'", resp.Content)
	}
}

func TestClient_ToolCallingExecutionLoop(t *testing.T) {
	tempDir := t.TempDir()
	dbMgr, _ := vault.NewDatabaseManager(filepath.Join(tempDir, "llm_tools.db"))
	_ = dbMgr.Initialize()
	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	v := vault.NewCredentialVault(dbMgr, cipher)
	limiter := dispatch.NewTokenBucketLimiter(nil)

	// Mock downstream Slack server
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "123.456"})
	}))
	defer slackServer.Close()

	dispatcher := dispatch.NewEgressDispatcher(v, limiter, slackServer.Client())
	tools := mcp.NewTools(dispatcher, nil, nil)

	// Mock LLM server: Turn 1 returns tool call -> Turn 2 returns final answer
	turns := 0
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turns++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if turns == 1 {
			// Return tool call for slack_post_message
			argsJSON, _ := json.Marshal(map[string]any{
				"channel_id": "C100",
				"text":       "Hello from agent",
				"url":        slackServer.URL,
			})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_slack_001",
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
		} else {
			// Final response after tool execution
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "Message has been posted to Slack successfully.",
						},
					},
				},
			})
		}
	}))
	defer llmServer.Close()

	client := llm.NewClient(llm.ClientConfig{
		Provider:   llm.ProviderOpenAI,
		APIKey:     "test-key",
		BaseURL:    llmServer.URL,
		HTTPClient: llmServer.Client(),
	})

	req := llm.ChatRequest{
		Model: "gpt-4o",
		Messages: []llm.Message{
			{Role: "user", Content: "Post hello to Slack C100"},
		},
		Tools: tools.ListTools(),
	}

	finalResp, err := client.ChatWithToolExecution(context.Background(), req, tools, 5)
	if err != nil {
		t.Fatalf("tool execution loop failed: %v", err)
	}

	if finalResp.Content != "Message has been posted to Slack successfully." {
		t.Fatalf("expected final response from agent, got '%s'", finalResp.Content)
	}
	if turns != 2 {
		t.Fatalf("expected exactly 2 LLM turns (tool call + final answer), got %d", turns)
	}
}
