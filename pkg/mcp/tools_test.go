package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/vault"
)

func setupTestTools(t *testing.T) (*mcp.Tools, *httptest.Server, *vault.CredentialVault) {
	tempDir := t.TempDir()
	dbMgr, err := vault.NewDatabaseManager(filepath.Join(tempDir, "mcp.db"))
	if err != nil {
		t.Fatalf("db error: %v", err)
	}
	_ = dbMgr.Initialize()

	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	v := vault.NewCredentialVault(dbMgr, cipher)
	limiter := dispatch.NewTokenBucketLimiter(nil)

	var lastReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReq = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": r.URL.Path})
	}))

	dispatcher := dispatch.NewEgressDispatcher(v, limiter, server.Client())
	tools := mcp.NewTools(dispatcher, map[string]string{}, map[string]string{})
	_ = lastReq

	return tools, server, v
}

func TestTools_ListTools(t *testing.T) {
	tools, server, _ := setupTestTools(t)
	defer server.Close()

	list := tools.ListTools()
	if len(list) < 4 {
		t.Fatalf("expected at least 4 tools, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, tool := range list {
		names[tool.Name] = true
	}

	expected := []string{"slack_post_message", "jira_create_issue", "twitter_post_tweet", "discord_send_message"}
	for _, exp := range expected {
		if !names[exp] {
			t.Fatalf("expected tool '%s' in tools/list", exp)
		}
	}
}

func TestTools_CallTool_SlackPostMessage(t *testing.T) {
	tools, server, v := setupTestTools(t)
	defer server.Close()

	tokID, _ := v.StoreOAuthToken("slack", "client_slack", map[string]any{"access_token": "xoxb-real"}, 3600)

	args := map[string]any{
		"channel_id": "C999",
		"text":       "Hello from MCP tool call",
		"token_id":   tokID,
		"url":        server.URL + "/api/chat.postMessage",
	}

	receipt, err := tools.CallTool(context.Background(), "slack_post_message", args)
	if err != nil {
		t.Fatalf("unexpected error calling tool: %v", err)
	}

	if receipt.Status != "success" || receipt.StatusCode != 200 {
		t.Fatalf("expected success 200 receipt, got %+v", receipt)
	}
}

func TestTools_CallTool_UnknownTool(t *testing.T) {
	tools, server, _ := setupTestTools(t)
	defer server.Close()

	_, err := tools.CallTool(context.Background(), "non_existent_tool", nil)
	if err == nil {
		t.Fatalf("expected error for unknown tool, got nil")
	}
}
