package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawbridge/pkg/bridge"
	"clawbridge/pkg/daemon"
	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/vault"
)

func TestE2E_FullLifecycleHarness(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "vault.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbMgr.Close()
	if err := dbMgr.Initialize(); err != nil {
		t.Fatal(err)
	}

	key, err := vault.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.NewEnvelopeCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	v := vault.NewCredentialVault(dbMgr, cipher)

	// 1. Mock Downstream Slack & Jira Servers
	var slackCalls int32
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&slackCalls, 1)
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "12345.67890"})
	}))
	defer slackServer.Close()

	var jiraCalls int32
	jiraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&jiraCalls, 1)
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Basic ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10001", "key": "PROJ-123"})
	}))
	defer jiraServer.Close()

	// 2. Store Credentials in Encrypted Vault
	_, err = v.UpsertCredentialByChannel("slack", "bearer", map[string]any{
		"token":          "xoxb-e2e-test-token",
		"signing_secret": "slack-secret-e2e",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = v.UpsertCredentialByChannel("jira", "basic_auth", map[string]any{
		"email":    "bot@org.com",
		"token":    "jira-api-token-e2e",
		"base_url": jiraServer.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Egress Dispatcher & MCP Tools
	limiter := dispatch.NewTokenBucketLimiter(nil)
	dispatcher := dispatch.NewEgressDispatcher(v, limiter, http.DefaultClient)
	tools := mcp.NewTools(dispatcher, nil, nil)

	// 4. Test Direct Tool Invocations
	ctx := context.Background()
	slackRes, err := tools.CallTool(ctx, "slack_post_message", map[string]any{
		"channel_id": "C100",
		"text":       "E2E test message",
		"url":        slackServer.URL,
	})
	if err != nil {
		t.Fatalf("slack tool call failed: %v", err)
	}
	if slackRes.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", slackRes.StatusCode)
	}

	jiraRes, err := tools.CallTool(ctx, "jira_create_issue", map[string]any{
		"project_key": "PROJ",
		"summary":     "E2E bug report",
		"url":         jiraServer.URL,
	})
	if err != nil {
		t.Fatalf("jira tool call failed: %v", err)
	}
	if jiraRes.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", jiraRes.StatusCode)
	}

	// 5. Inbound Webhook Verification & SSE Broadcasting
	broadcaster := inbound.NewEventBroadcaster()
	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	normalizer := inbound.NewEventNormalizer()

	rawBody := `{"event":{"type":"message","user":"U123","text":"customer inquiry"}}`
	nowTs := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte("slack-secret-e2e"))
	mac.Write([]byte(fmt.Sprintf("v0:%s:%s", nowTs, rawBody)))
	validSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	verified, err := verifier.VerifySlack([]byte(rawBody), validSig, nowTs, "slack-secret-e2e", "delivery-12345")
	if err != nil || verified == nil {
		t.Fatalf("webhook verification failed: %v", err)
	}

	inboundEvt, err := normalizer.Normalize(verified)
	if err != nil {
		t.Fatalf("normalization failed: %v", err)
	}

	// Subscribe and publish
	subChan := broadcaster.Subscribe([]string{"slack:*"})
	broadcaster.Publish(inboundEvt)

	select {
	case received := <-subChan:
		if received.Channel != "slack" {
			t.Errorf("expected channel slack, got %s", received.Channel)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for broadcast event")
	}

	// 6. Universal Bridge & MCP Integration (OpenClaw & NVIDIA NemoClaw)
	openClawCfg, err := bridge.ConfigureOpenClawBridge(filepath.Join(tempDir, ".openclaw"), "/usr/local/bin/clawbridge")
	if err != nil {
		t.Fatalf("failed to configure openclaw bridge: %v", err)
	}
	if _, err := os.Stat(openClawCfg); err != nil {
		t.Fatalf("expected openclaw.json to exist at %s: %v", openClawCfg, err)
	}

	nemoPolicy, err := bridge.ConfigureNemoClawBridge(filepath.Join(tempDir, ".nemoclaw"), "/usr/local/bin/clawbridge")
	if err != nil {
		t.Fatalf("failed to configure nemoclaw bridge: %v", err)
	}
	if _, err := os.Stat(nemoPolicy); err != nil {
		t.Fatalf("expected sandbox.yaml to exist at %s: %v", nemoPolicy, err)
	}

	// 7. Supervisor Auto-Recovery
	sup := daemon.NewSupervisor(10*time.Millisecond, nil)
	workerDone := make(chan struct{})
	var runs int32

	supCtx, supCancel := context.WithCancel(context.Background())
	defer supCancel()

	go sup.Supervise(supCtx, "recovery-bot", func(wCtx context.Context) error {
		r := atomic.AddInt32(&runs, 1)
		if r == 1 {
			panic("simulated fatal worker crash")
		}
		select {
		case <-workerDone:
		default:
			close(workerDone)
		}
		<-wCtx.Done()
		return nil
	})

	select {
	case <-workerDone:
		// Successfully restarted after panic
	case <-time.After(1 * time.Second):
		t.Fatalf("supervisor failed to recover from panic")
	}

	if sup.PanicCount("recovery-bot") != 1 {
		t.Errorf("expected 1 panic recorded, got %d", sup.PanicCount("recovery-bot"))
	}
}
