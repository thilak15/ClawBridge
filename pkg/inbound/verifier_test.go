package inbound_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"clawbridge/pkg/inbound"
	"clawbridge/pkg/vault"
)

func setupTestDB(t *testing.T) *vault.DatabaseManager {
	tempDir := t.TempDir()
	dbMgr, err := vault.NewDatabaseManager(filepath.Join(tempDir, "inbound.db"))
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}
	if err := dbMgr.Initialize(); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	return dbMgr
}

func computeSlackSignature(secret, timestamp string, body []byte) string {
	base := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerifier_VerifySlackSuccess(t *testing.T) {
	dbMgr := setupTestDB(t)
	defer dbMgr.Close()

	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	secret := "test-slack-signing-secret"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"type":"event_callback","event":{"type":"message","text":"hello agent"}}`)
	sig := computeSlackSignature(secret, ts, body)

	res, err := verifier.VerifySlack(body, sig, ts, secret, "deliv_slack_001")
	if err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}

	if res.Channel != "slack" || res.DeliveryID != "deliv_slack_001" || !res.IsValid {
		t.Fatalf("unexpected verified webhook struct: %+v", res)
	}
}

func TestWebhookVerifier_VerifySlackTamperedSignatureFails(t *testing.T) {
	dbMgr := setupTestDB(t)
	defer dbMgr.Close()

	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"hello":"world"}`)

	_, err := verifier.VerifySlack(body, "v0=invalidhash123", ts, "secret", "deliv_002")
	if err == nil {
		t.Fatalf("expected signature mismatch error, got nil")
	}
}

func TestWebhookVerifier_StaleTimestampRejected(t *testing.T) {
	dbMgr := setupTestDB(t)
	defer dbMgr.Close()

	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	secret := "secret"
	// Timestamp 305 seconds in the past
	staleTS := strconv.FormatInt(time.Now().Unix()-305, 10)
	body := []byte(`{"hello":"world"}`)
	sig := computeSlackSignature(secret, staleTS, body)

	_, err := verifier.VerifySlack(body, sig, staleTS, secret, "deliv_stale")
	if err == nil {
		t.Fatalf("expected stale timestamp error, got nil")
	}
}

func TestWebhookVerifier_ReplayDeduplicationRejected(t *testing.T) {
	dbMgr := setupTestDB(t)
	defer dbMgr.Close()

	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	secret := "secret"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"hello":"world"}`)
	sig := computeSlackSignature(secret, ts, body)

	// First delivery succeeds
	_, err := verifier.VerifySlack(body, sig, ts, secret, "duplicate_deliv_id")
	if err != nil {
		t.Fatalf("first delivery failed: %v", err)
	}

	// Immediate replay with same delivery_id must fail
	_, err = verifier.VerifySlack(body, sig, ts, secret, "duplicate_deliv_id")
	if err == nil {
		t.Fatalf("expected duplicate delivery to be rejected, got nil")
	}
}

func TestWebhookVerifier_VerifyHMACSHA256_GenericGitHub(t *testing.T) {
	dbMgr := setupTestDB(t)
	defer dbMgr.Close()

	verifier := inbound.NewWebhookVerifier(dbMgr, 300)
	secret := "github-webhook-secret"
	body := []byte(`{"action":"opened","issue":{"number":42}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sigHeader := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := verifier.VerifyHMACSHA256("github", body, sigHeader, secret, "gh_deliv_001", "sha256=")
	if err != nil {
		t.Fatalf("expected valid github hmac, got: %v", err)
	}
	if res.Channel != "github" || res.DeliveryID != "gh_deliv_001" {
		t.Fatalf("unexpected result: %+v", res)
	}
}
