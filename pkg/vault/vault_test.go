package vault_test

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbridge/pkg/vault"
)

func setupTestVault(t *testing.T) (*vault.CredentialVault, *vault.DatabaseManager) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_vault.db")

	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}

	if err := dbMgr.Initialize(); err != nil {
		t.Fatalf("failed to initialize db: %v", err)
	}

	key, err := vault.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := vault.NewEnvelopeCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	v := vault.NewCredentialVault(dbMgr, cipher)
	return v, dbMgr
}

func TestCredentialVault_StoreAndGetCredential(t *testing.T) {
	v, dbMgr := setupTestVault(t)
	defer dbMgr.Close()

	credData := map[string]any{
		"token": "xoxb-real-slack-secret-12345",
	}

	id, err := v.StoreCredential("slack", "bot_token", credData)
	if err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}
	if !strings.HasPrefix(id, "cred_") {
		t.Fatalf("expected id starting with cred_, got %s", id)
	}

	// Verify database row contains encrypted blob, not raw plaintext
	var rawBlob string
	err = dbMgr.DB().QueryRow("SELECT encrypted_blob FROM credentials WHERE id = ?", id).Scan(&rawBlob)
	if err != nil {
		t.Fatalf("failed to query raw row: %v", err)
	}
	if strings.Contains(rawBlob, "xoxb-real-slack-secret-12345") {
		t.Fatalf("raw plaintext leaked into database! Got: %s", rawBlob)
	}

	// Retrieve through vault
	cred, err := v.GetCredential(id)
	if err != nil {
		t.Fatalf("failed to retrieve credential: %v", err)
	}

	if cred.Channel != "slack" || cred.AuthType != "bot_token" {
		t.Fatalf("mismatched metadata: channel=%s, authType=%s", cred.Channel, cred.AuthType)
	}
	if cred.Data["token"] != "xoxb-real-slack-secret-12345" {
		t.Fatalf("expected token preserved, got %v", cred.Data["token"])
	}
}

func TestCredentialVault_StoreAndGetOAuthToken(t *testing.T) {
	v, dbMgr := setupTestVault(t)
	defer dbMgr.Close()

	tokenData := map[string]any{
		"access_token":  "gho_test_github_access_token",
		"refresh_token": "ghr_test_refresh_token",
	}

	id, err := v.StoreOAuthToken("github", "client_gh_001", tokenData, 3600)
	if err != nil {
		t.Fatalf("failed to store oauth token: %v", err)
	}
	if !strings.HasPrefix(id, "tok_") {
		t.Fatalf("expected id starting with tok_, got %s", id)
	}

	rec, err := v.GetOAuthToken(id)
	if err != nil {
		t.Fatalf("failed to retrieve oauth token: %v", err)
	}

	if rec.Channel != "github" || rec.ClientID != "client_gh_001" {
		t.Fatalf("mismatched metadata: channel=%s, clientID=%s", rec.Channel, rec.ClientID)
	}
	if rec.TokenData["access_token"] != "gho_test_github_access_token" {
		t.Fatalf("expected access token preserved, got %v", rec.TokenData["access_token"])
	}
	if rec.IsExpired() {
		t.Fatalf("token with 3600s TTL should not be expired")
	}
}

func TestCredentialVault_GetOrRefreshOAuthToken_Deduplicated(t *testing.T) {
	v, dbMgr := setupTestVault(t)
	defer dbMgr.Close()

	// Store an already-expired token (-60s)
	tokenData := map[string]any{
		"access_token":  "expired_access_token",
		"refresh_token": "valid_refresh_token",
	}
	id, err := v.StoreOAuthToken("google", "client_g_001", tokenData, -60)
	if err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	var refreshCount int64
	refreshFn := func(rec *vault.OAuthTokenRecord) (map[string]any, error) {
		atomic.AddInt64(&refreshCount, 1)
		time.Sleep(50 * time.Millisecond) // network simulation
		return map[string]any{
			"access_token":  "brand_new_refreshed_token",
			"refresh_token": "valid_refresh_token",
		}, nil
	}

	// Launch 20 concurrent readers calling GetOrRefreshOAuthToken
	const concurrentCallers = 20
	var wg sync.WaitGroup
	wg.Add(concurrentCallers)

	tokens := make([]*vault.OAuthTokenRecord, concurrentCallers)
	for i := 0; i < concurrentCallers; i++ {
		go func(idx int) {
			defer wg.Done()
			rec, err := v.GetOrRefreshOAuthToken(id, refreshFn)
			if err != nil {
				t.Errorf("caller %d failed: %v", idx, err)
			}
			tokens[idx] = rec
		}(i)
	}
	wg.Wait()

	if count := atomic.LoadInt64(&refreshCount); count != 1 {
		t.Fatalf("expected refresh function called exactly once, called %d times", count)
	}

	for i := 0; i < concurrentCallers; i++ {
		if tokens[i].TokenData["access_token"] != "brand_new_refreshed_token" {
			t.Fatalf("caller %d expected refreshed token, got %v", i, tokens[i].TokenData["access_token"])
		}
	}
}
