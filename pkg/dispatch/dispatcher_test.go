package dispatch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/vault"
)

func setupTestDispatcher(t *testing.T) (*dispatch.EgressDispatcher, *vault.CredentialVault, *dispatch.TokenBucketLimiter) {
	tempDir := t.TempDir()
	dbMgr, err := vault.NewDatabaseManager(filepath.Join(tempDir, "disp.db"))
	if err != nil {
		t.Fatalf("db error: %v", err)
	}
	_ = dbMgr.Initialize()

	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	v := vault.NewCredentialVault(dbMgr, cipher)
	limiter := dispatch.NewTokenBucketLimiter(nil)

	dispatcher := dispatch.NewEgressDispatcher(v, limiter, http.DefaultClient)
	return dispatcher, v, limiter
}

func TestEgressDispatcher_DispatchSuccessWithJITCredential(t *testing.T) {
	dispatcher, v, _ := setupTestDispatcher(t)

	// Create mock downstream API server
	var receivedAuthHeader string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message_id": "msg_123"})
	}))
	defer mockServer.Close()

	// Store credential in Vault
	credID, err := v.StoreCredential("slack", "bot_token", map[string]any{"token": "xoxb-real-secret"})
	if err != nil {
		t.Fatalf("failed to store cred: %v", err)
	}

	req := dispatch.DispatchRequest{
		Channel:      "slack",
		Action:       "post_message",
		Method:       http.MethodPost,
		URL:          mockServer.URL + "/api/chat.postMessage",
		CredentialID: credID,
		Body:         map[string]any{"channel": "C123", "text": "hello"},
	}

	receipt, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receipt.Status != "success" || receipt.StatusCode != 200 {
		t.Fatalf("expected success 200 receipt, got %+v", receipt)
	}
	if receivedAuthHeader != "Bearer xoxb-real-secret" {
		t.Fatalf("expected injected auth header 'Bearer xoxb-real-secret', got '%s'", receivedAuthHeader)
	}
}

func TestEgressDispatcher_RateLimitBlocksDispatchLocally(t *testing.T) {
	tempDir := t.TempDir()
	dbMgr, _ := vault.NewDatabaseManager(filepath.Join(tempDir, "disp2.db"))
	_ = dbMgr.Initialize()
	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	v := vault.NewCredentialVault(dbMgr, cipher)

	// Rate limiter with zero tokens
	limiter := dispatch.NewTokenBucketLimiter(map[string]dispatch.ChannelRate{
		"slack": {RatePerSecond: 1.0, Capacity: 0.0},
	})
	dispatcher := dispatch.NewEgressDispatcher(v, limiter, http.DefaultClient)

	req := dispatch.DispatchRequest{
		Channel: "slack",
		Action:  "post_message",
		Method:  http.MethodPost,
		URL:     "https://slack.com/api/test",
	}

	receipt, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no go error, receipt returned instead")
	}

	if receipt.Status != "error" || receipt.Code != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED error receipt, got %+v", receipt)
	}
	if receipt.RetryAfterS <= 0 {
		t.Fatalf("expected positive retry_after_s, got %v", receipt.RetryAfterS)
	}
}

func TestEgressDispatcher_Downstream429RecordsThrottle(t *testing.T) {
	dispatcher, _, limiter := setupTestDispatcher(t)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": "rate_limited"}`))
	}))
	defer mockServer.Close()

	req := dispatch.DispatchRequest{
		Channel: "twitter",
		Action:  "post_tweet",
		Method:  http.MethodPost,
		URL:     mockServer.URL,
	}

	receipt, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receipt.Code != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED receipt, got %+v", receipt)
	}

	// Channel should now be throttled in limiter
	if limiter.IsAvailable("twitter") {
		t.Fatalf("expected twitter to be throttled after downstream 429")
	}
}

func TestEgressDispatcher_RetryTransientErrors(t *testing.T) {
	dispatcher, _, _ := setupTestDispatcher(t)

	var attempts int64
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt64(&attempts, 1)
		if call < 3 {
			// Simulate transient failure (503 Service Unavailable)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// 3rd attempt succeeds
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"recovered": true})
	}))
	defer mockServer.Close()

	req := dispatch.DispatchRequest{
		Channel: "jira",
		Action:  "create_issue",
		Method:  http.MethodPost,
		URL:     mockServer.URL,
	}

	receipt, err := dispatcher.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receipt.Status != "success" {
		t.Fatalf("expected retry loop to recover and succeed, got %+v", receipt)
	}
	if count := atomic.LoadInt64(&attempts); count != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", count)
	}
}
