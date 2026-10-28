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

	expected := []string{
		"slack_post_message",
		"jira_create_issue",
		"twitter_post_tweet",
		"discord_send_message",
		"telegram_send_message",
		"telegram_send_photo",
		"whatsapp_send_message",
		"whatsapp_send_template",
	}
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

func TestTools_CallTool_TelegramAndWhatsApp(t *testing.T) {
	tools, server, v := setupTestTools(t)
	defer server.Close()

	credID, _ := v.StoreCredential("telegram", "bot_token", map[string]any{"token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"})
	waCredID, _ := v.StoreCredential("whatsapp", "bearer", map[string]any{"token": "EAABwa123456"})

	// 1. Telegram send message
	tgArgs := map[string]any{
		"chat_id":       "@channel_alerts",
		"text":          "Deployment complete",
		"credential_id": credID,
		"url":           server.URL + "/bot/sendMessage",
	}
	tgReceipt, err := tools.CallTool(context.Background(), "telegram_send_message", tgArgs)
	if err != nil || tgReceipt.StatusCode != 200 {
		t.Fatalf("failed to call telegram_send_message: err=%v, receipt=%+v", err, tgReceipt)
	}

	// 2. Telegram send photo
	photoArgs := map[string]any{
		"chat_id":       "@channel_alerts",
		"photo_url":     "https://example.com/status.png",
		"caption":       "System healthy",
		"credential_id": credID,
		"url":           server.URL + "/bot/sendPhoto",
	}
	photoReceipt, err := tools.CallTool(context.Background(), "telegram_send_photo", photoArgs)
	if err != nil || photoReceipt.StatusCode != 200 {
		t.Fatalf("failed to call telegram_send_photo: err=%v, receipt=%+v", err, photoReceipt)
	}

	// 3. WhatsApp send message
	waArgs := map[string]any{
		"to":              "+14155552671",
		"text":            "Order confirmed",
		"phone_number_id": "100200300",
		"credential_id":   waCredID,
		"url":             server.URL + "/v20.0/messages",
	}
	waReceipt, err := tools.CallTool(context.Background(), "whatsapp_send_message", waArgs)
	if err != nil || waReceipt.StatusCode != 200 {
		t.Fatalf("failed to call whatsapp_send_message: err=%v, receipt=%+v", err, waReceipt)
	}

	// 4. WhatsApp send template
	tmplArgs := map[string]any{
		"to":              "+14155552671",
		"template_name":   "sample_shipping_confirmation",
		"language_code":   "en_US",
		"phone_number_id": "100200300",
		"credential_id":   waCredID,
		"url":             server.URL + "/v20.0/messages",
	}
	tmplReceipt, err := tools.CallTool(context.Background(), "whatsapp_send_template", tmplArgs)
	if err != nil || tmplReceipt.StatusCode != 200 {
		t.Fatalf("failed to call whatsapp_send_template: err=%v, receipt=%+v", err, tmplReceipt)
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
