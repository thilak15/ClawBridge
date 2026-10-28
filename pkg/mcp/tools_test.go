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
		"instagram_post_publish",
		"instagram_read_comments",
		"instagram_reply_comment",
		"facebook_post_feed",
		"facebook_read_page_feed",
		"calendar_list_events",
		"calendar_find_free_slots",
		"calendar_create_event",
		"gmail_search_messages",
		"gmail_read_thread",
		"gmail_create_draft",
		"gmail_send_message",
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

func TestTools_CallTool_MetaInstagramAndFacebook(t *testing.T) {
	tools, server, v := setupTestTools(t)
	defer server.Close()

	igTokenID, _ := v.StoreOAuthToken("instagram", "ig_client", map[string]any{"access_token": "IG_ACCESS_TOKEN_123"}, 3600)
	fbTokenID, _ := v.StoreOAuthToken("facebook", "fb_client", map[string]any{"access_token": "EAABfb123456"}, 3600)

	// 1. Instagram Post Publish
	igPostArgs := map[string]any{
		"image_url": "https://example.com/banner.jpg",
		"caption":   "Exciting launch! #ai #daemon",
		"token_id":  igTokenID,
		"url":       server.URL + "/v20.0/me/media",
	}
	igReceipt, err := tools.CallTool(context.Background(), "instagram_post_publish", igPostArgs)
	if err != nil || igReceipt.StatusCode != 200 {
		t.Fatalf("failed to call instagram_post_publish: err=%v, receipt=%+v", err, igReceipt)
	}

	// 2. Instagram Read Comments
	igReadArgs := map[string]any{
		"media_id": "media_987654",
		"token_id": igTokenID,
		"url":      server.URL + "/v20.0/media_987654/comments",
	}
	readReceipt, err := tools.CallTool(context.Background(), "instagram_read_comments", igReadArgs)
	if err != nil || readReceipt.StatusCode != 200 {
		t.Fatalf("failed to call instagram_read_comments: err=%v, receipt=%+v", err, readReceipt)
	}

	// 3. Instagram Reply Comment
	igReplyArgs := map[string]any{
		"comment_id": "cmt_112233",
		"message":    "Thank you for the feedback!",
		"token_id":   igTokenID,
		"url":        server.URL + "/v20.0/cmt_112233/replies",
	}
	replyReceipt, err := tools.CallTool(context.Background(), "instagram_reply_comment", igReplyArgs)
	if err != nil || replyReceipt.StatusCode != 200 {
		t.Fatalf("failed to call instagram_reply_comment: err=%v, receipt=%+v", err, replyReceipt)
	}

	// 4. Facebook Post Feed
	fbPostArgs := map[string]any{
		"message":  "Release v1.0 is now live!",
		"link":     "https://clawbridge.dev",
		"token_id": fbTokenID,
		"url":      server.URL + "/v20.0/me/feed",
	}
	fbReceipt, err := tools.CallTool(context.Background(), "facebook_post_feed", fbPostArgs)
	if err != nil || fbReceipt.StatusCode != 200 {
		t.Fatalf("failed to call facebook_post_feed: err=%v, receipt=%+v", err, fbReceipt)
	}

	// 5. Facebook Read Page Feed
	fbReadArgs := map[string]any{
		"page_id":  "me",
		"token_id": fbTokenID,
		"url":      server.URL + "/v20.0/me/feed",
	}
	fbReadReceipt, err := tools.CallTool(context.Background(), "facebook_read_page_feed", fbReadArgs)
	if err != nil || fbReadReceipt.StatusCode != 200 {
		t.Fatalf("failed to call facebook_read_page_feed: err=%v, receipt=%+v", err, fbReadReceipt)
	}
}

func TestTools_CallTool_GoogleCalendarAndGmail(t *testing.T) {
	tools, server, v := setupTestTools(t)
	defer server.Close()

	googleTokenID, _ := v.StoreOAuthToken("google", "client_google", map[string]any{"access_token": "ya29.google-test-token"}, 3600)

	// 1. Calendar list events
	calListArgs := map[string]any{
		"calendar_id": "primary",
		"time_min":    "2026-10-28T00:00:00Z",
		"token_id":    googleTokenID,
		"url":         server.URL + "/calendar/v3/calendars/primary/events",
	}
	calListReceipt, err := tools.CallTool(context.Background(), "calendar_list_events", calListArgs)
	if err != nil || calListReceipt.StatusCode != 200 {
		t.Fatalf("failed to call calendar_list_events: err=%v, receipt=%+v", err, calListReceipt)
	}

	// 2. Calendar find free slots
	calFreeArgs := map[string]any{
		"calendar_id": "primary",
		"time_min":    "2026-10-28T09:00:00Z",
		"time_max":    "2026-10-28T17:00:00Z",
		"token_id":    googleTokenID,
		"url":         server.URL + "/calendar/v3/freeBusy",
	}
	calFreeReceipt, err := tools.CallTool(context.Background(), "calendar_find_free_slots", calFreeArgs)
	if err != nil || calFreeReceipt.StatusCode != 200 {
		t.Fatalf("failed to call calendar_find_free_slots: err=%v, receipt=%+v", err, calFreeReceipt)
	}

	// 3. Calendar create event
	calCreateArgs := map[string]any{
		"summary":     "Sprint Planning",
		"description": "Weekly alignment",
		"start_time":  "2026-10-29T10:00:00Z",
		"end_time":    "2026-10-29T11:00:00Z",
		"attendees":   []any{"colleague@example.com"},
		"token_id":    googleTokenID,
		"url":         server.URL + "/calendar/v3/calendars/primary/events",
	}
	calCreateReceipt, err := tools.CallTool(context.Background(), "calendar_create_event", calCreateArgs)
	if err != nil || calCreateReceipt.StatusCode != 200 {
		t.Fatalf("failed to call calendar_create_event: err=%v, receipt=%+v", err, calCreateReceipt)
	}

	// 4. Gmail search messages
	gmailSearchArgs := map[string]any{
		"query":    "is:unread label:inbox",
		"token_id": googleTokenID,
		"url":      server.URL + "/gmail/v1/users/me/messages",
	}
	gmailSearchReceipt, err := tools.CallTool(context.Background(), "gmail_search_messages", gmailSearchArgs)
	if err != nil || gmailSearchReceipt.StatusCode != 200 {
		t.Fatalf("failed to call gmail_search_messages: err=%v, receipt=%+v", err, gmailSearchReceipt)
	}

	// 5. Gmail read thread
	gmailThreadArgs := map[string]any{
		"thread_id": "thread_abc123",
		"token_id":  googleTokenID,
		"url":       server.URL + "/gmail/v1/users/me/threads/thread_abc123",
	}
	gmailThreadReceipt, err := tools.CallTool(context.Background(), "gmail_read_thread", gmailThreadArgs)
	if err != nil || gmailThreadReceipt.StatusCode != 200 {
		t.Fatalf("failed to call gmail_read_thread: err=%v, receipt=%+v", err, gmailThreadReceipt)
	}

	// 6. Gmail create draft
	gmailDraftArgs := map[string]any{
		"to":       "partner@example.com",
		"subject":  "Draft Partnership Proposal",
		"body":     "Here is the tentative draft proposal.",
		"token_id": googleTokenID,
		"url":      server.URL + "/gmail/v1/users/me/drafts",
	}
	gmailDraftReceipt, err := tools.CallTool(context.Background(), "gmail_create_draft", gmailDraftArgs)
	if err != nil || gmailDraftReceipt.StatusCode != 200 {
		t.Fatalf("failed to call gmail_create_draft: err=%v, receipt=%+v", err, gmailDraftReceipt)
	}

	// 7. Gmail send message
	gmailSendArgs := map[string]any{
		"to":       "client@example.com",
		"subject":  "Product Launch Update",
		"body":     "The product launch has been completed successfully.",
		"token_id": googleTokenID,
		"url":      server.URL + "/gmail/v1/users/me/messages/send",
	}
	gmailSendReceipt, err := tools.CallTool(context.Background(), "gmail_send_message", gmailSendArgs)
	if err != nil || gmailSendReceipt.StatusCode != 200 {
		t.Fatalf("failed to call gmail_send_message: err=%v, receipt=%+v", err, gmailSendReceipt)
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
