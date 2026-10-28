package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnv(t *testing.T) {
	raw := `
# Comment line
SLACK_BOT_TOKEN="xoxb-test-12345"
SLACK_SIGNING_SECRET='secret_abc_123'
JIRA_BASE_URL=https://myorg.atlassian.net
JIRA_EMAIL=dev@myorg.com
JIRA_API_TOKEN=api_token_xyz
DISCORD_BOT_TOKEN=discord_bot_token_789
TWITTER_BEARER_TOKEN="bearer_token_tweet"
GITHUB_TOKEN=ghp_github_test_token
# Trailing comment
`

	envMap, err := ParseEnv(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error parsing env: %v", err)
	}

	if envMap["SLACK_BOT_TOKEN"] != "xoxb-test-12345" {
		t.Errorf("expected xoxb-test-12345, got %s", envMap["SLACK_BOT_TOKEN"])
	}
	if envMap["SLACK_SIGNING_SECRET"] != "secret_abc_123" {
		t.Errorf("expected secret_abc_123, got %s", envMap["SLACK_SIGNING_SECRET"])
	}
	if envMap["JIRA_BASE_URL"] != "https://myorg.atlassian.net" {
		t.Errorf("expected https://myorg.atlassian.net, got %s", envMap["JIRA_BASE_URL"])
	}
	if envMap["GITHUB_TOKEN"] != "ghp_github_test_token" {
		t.Errorf("expected ghp_github_test_token, got %s", envMap["GITHUB_TOKEN"])
	}
}

func TestSyncEnvVars_EncryptsAndRetrievesCredentials(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-vault-sync-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbMgr.Close()
	if err := dbMgr.Initialize(); err != nil {
		t.Fatal(err)
	}

	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewEnvelopeCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	v := NewCredentialVault(dbMgr, cipher)

	envVars := map[string]string{
		"SLACK_BOT_TOKEN":      "xoxb-test-token",
		"SLACK_SIGNING_SECRET": "test-slack-secret",
		"JIRA_BASE_URL":        "https://myco.atlassian.net",
		"JIRA_EMAIL":           "agent@myco.com",
		"JIRA_API_TOKEN":       "jira-secret-key",
		"DISCORD_BOT_TOKEN":    "discord-token-xyz",
		"TWITTER_BEARER_TOKEN": "twitter-bearer-val",
		"GITHUB_TOKEN":         "github-pat-token",
	}

	synced, err := SyncEnvVars(v, envVars)
	if err != nil {
		t.Fatalf("unexpected error during sync: %v", err)
	}

	if len(synced) != 5 {
		t.Fatalf("expected 5 synced channels, got %d: %v", len(synced), synced)
	}

	// Verify Slack credential in credentials table
	slackCred, err := v.GetCredentialByChannel("slack")
	if err != nil {
		t.Fatalf("failed to get slack credential: %v", err)
	}
	if slackCred.Data["token"] != "xoxb-test-token" {
		t.Errorf("expected xoxb-test-token, got %v", slackCred.Data["token"])
	}
	if slackCred.Data["signing_secret"] != "test-slack-secret" {
		t.Errorf("expected test-slack-secret, got %v", slackCred.Data["signing_secret"])
	}

	// Verify Slack token in oauth_tokens table
	slackOAuth, err := v.GetOAuthTokenByChannel("slack")
	if err != nil {
		t.Fatalf("failed to get slack oauth token: %v", err)
	}
	if slackOAuth.TokenData["access_token"] != "xoxb-test-token" {
		t.Errorf("expected xoxb-test-token, got %v", slackOAuth.TokenData["access_token"])
	}

	// Verify Jira credential
	jiraCred, err := v.GetCredentialByChannel("jira")
	if err != nil {
		t.Fatalf("failed to get jira credential: %v", err)
	}
	if jiraCred.Data["email"] != "agent@myco.com" || jiraCred.Data["token"] != "jira-secret-key" {
		t.Errorf("unexpected jira credential data: %v", jiraCred.Data)
	}

	// Verify Discord credential
	discordCred, err := v.GetCredentialByChannel("discord")
	if err != nil {
		t.Fatalf("failed to get discord credential: %v", err)
	}
	if discordCred.Data["token"] != "discord-token-xyz" {
		t.Errorf("unexpected discord credential token: %v", discordCred.Data["token"])
	}

	// Verify Twitter credential
	twitterCred, err := v.GetCredentialByChannel("twitter")
	if err != nil {
		t.Fatalf("failed to get twitter credential: %v", err)
	}
	if twitterCred.Data["token"] != "twitter-bearer-val" {
		t.Errorf("unexpected twitter credential token: %v", twitterCred.Data["token"])
	}

	// Verify GitHub credential
	githubCred, err := v.GetCredentialByChannel("github")
	if err != nil {
		t.Fatalf("failed to get github credential: %v", err)
	}
	if githubCred.Data["token"] != "github-pat-token" {
		t.Errorf("unexpected github credential token: %v", githubCred.Data["token"])
	}
}

func TestAutoSyncEnv_FromFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-autosync-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbMgr.Close()
	_ = dbMgr.Initialize()

	key, _ := GenerateKey()
	cipher, _ := NewEnvelopeCipher(key)
	v := NewCredentialVault(dbMgr, cipher)

	envFile := filepath.Join(tmpDir, ".env")
	envContent := `
DISCORD_BOT_TOKEN=discord_from_file_123
GITHUB_TOKEN=github_from_file_456
`
	if err := os.WriteFile(envFile, []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	synced, err := AutoSyncEnv(v, envFile)
	if err != nil {
		t.Fatalf("unexpected error auto syncing: %v", err)
	}

	foundDiscord := false
	foundGitHub := false
	for _, s := range synced {
		if s == "discord" {
			foundDiscord = true
		}
		if s == "github" {
			foundGitHub = true
		}
	}

	if !foundDiscord || !foundGitHub {
		t.Errorf("expected discord and github to be synced, got %v", synced)
	}

	discordCred, err := v.GetCredentialByChannel("discord")
	if err != nil {
		t.Fatalf("failed to retrieve discord cred: %v", err)
	}
	if discordCred.Data["token"] != "discord_from_file_123" {
		t.Errorf("expected discord_from_file_123, got %v", discordCred.Data["token"])
	}
}

func TestAutoSyncEnv_LLMCredentialsAndSaveEnvVar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-llm-sync-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbMgr.Close()
	_ = dbMgr.Initialize()

	key, _ := GenerateKey()
	cipher, _ := NewEnvelopeCipher(key)
	v := NewCredentialVault(dbMgr, cipher)

	envFile := filepath.Join(tmpDir, ".env")

	// Test SaveEnvVar
	if err := SaveEnvVar(envFile, "GEMINI_API_KEY", "AQ.TestAuthKey123"); err != nil {
		t.Fatalf("failed to save env var: %v", err)
	}
	if err := SaveEnvVar(envFile, "OPENAI_API_KEY", "sk-proj-test-123"); err != nil {
		t.Fatalf("failed to save env var: %v", err)
	}

	synced, err := AutoSyncEnv(v, envFile)
	if err != nil {
		t.Fatalf("unexpected error auto syncing: %v", err)
	}

	foundGemini := false
	foundOpenAI := false
	for _, s := range synced {
		if s == "gemini" {
			foundGemini = true
		}
		if s == "openai" {
			foundOpenAI = true
		}
	}

	if !foundGemini || !foundOpenAI {
		t.Errorf("expected gemini and openai to be synced, got %v", synced)
	}

	geminiCred, err := v.GetCredentialByChannel("gemini")
	if err != nil || geminiCred == nil {
		t.Fatalf("failed to retrieve gemini cred: %v", err)
	}
	if geminiCred.Data["key"] != "AQ.TestAuthKey123" {
		t.Errorf("expected 'AQ.TestAuthKey123', got %v", geminiCred.Data["key"])
	}
}

