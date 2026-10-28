package auth_test

import (
	"path/filepath"
	"strings"
	"testing"

	"clawbridge/pkg/auth"
	"clawbridge/pkg/vault"
)

func TestTokenManager_GenerateAndVerify(t *testing.T) {
	rawToken, tokenHash, err := auth.GenerateAgentToken()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	if !strings.HasPrefix(rawToken, "clw_agent_") {
		t.Fatalf("expected raw token prefix 'clw_agent_', got '%s'", rawToken)
	}
	if len(rawToken) < 40 {
		t.Fatalf("expected raw token length >= 40, got %d", len(rawToken))
	}
	if len(tokenHash) != 64 { // SHA-256 hex string
		t.Fatalf("expected 64-char hex digest, got %d", len(tokenHash))
	}

	// Verify valid token matches
	if !auth.VerifyAgentToken(rawToken, tokenHash) {
		t.Fatalf("expected token verification to succeed")
	}

	// Verify tampered token fails
	tampered := rawToken[:len(rawToken)-1] + "X"
	if auth.VerifyAgentToken(tampered, tokenHash) {
		t.Fatalf("expected tampered token to fail verification")
	}
}

func TestPolicyEngine_WildcardMatching(t *testing.T) {
	// Universal access rule "*"
	if !auth.IsAuthorized([]string{"*"}, "slack", "post_message") {
		t.Fatalf("expected universal rule '*' to authorize slack:post_message")
	}
	if !auth.IsAuthorized([]string{"*"}, "jira", "create_issue") {
		t.Fatalf("expected universal rule '*' to authorize jira:create_issue")
	}

	// Channel wildcard "slack:*"
	rules := []string{"slack:*", "jira:read_issue"}
	if !auth.IsAuthorized(rules, "slack", "post_message") {
		t.Fatalf("expected slack:* to match post_message")
	}
	if !auth.IsAuthorized(rules, "slack", "delete_channel") {
		t.Fatalf("expected slack:* to match delete_channel")
	}
	if !auth.IsAuthorized(rules, "jira", "read_issue") {
		t.Fatalf("expected jira:read_issue to match")
	}
	if auth.IsAuthorized(rules, "jira", "create_issue") {
		t.Fatalf("expected jira:create_issue to be denied")
	}
	if auth.IsAuthorized(rules, "twitter", "post_tweet") {
		t.Fatalf("expected twitter:post_tweet to be denied")
	}

	// Action wildcard "*:read*"
	readRules := []string{"*:read*"}
	if !auth.IsAuthorized(readRules, "jira", "read_issue") {
		t.Fatalf("expected *:read* to match jira:read_issue")
	}
	if !auth.IsAuthorized(readRules, "github", "read_repo") {
		t.Fatalf("expected *:read* to match github:read_repo")
	}
	if auth.IsAuthorized(readRules, "slack", "post_message") {
		t.Fatalf("expected *:read* to deny slack:post_message")
	}
}

func TestAgentRegistry_RegisterAndLookup(t *testing.T) {
	tempDir := t.TempDir()
	dbMgr, err := vault.NewDatabaseManager(filepath.Join(tempDir, "auth.db"))
	if err != nil {
		t.Fatalf("db error: %v", err)
	}
	defer dbMgr.Close()
	_ = dbMgr.Initialize()

	reg := auth.NewAgentRegistry(dbMgr)

	// Register agent NemoClaw with universal rules
	agent, rawToken, err := reg.RegisterAgent("nemoclaw", []string{"*"})
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	if agent.Name != "nemoclaw" {
		t.Fatalf("expected name 'nemoclaw', got '%s'", agent.Name)
	}
	if !strings.HasPrefix(rawToken, "clw_agent_") {
		t.Fatalf("expected clw_agent_ token, got '%s'", rawToken)
	}

	// Lookup by Name
	foundByName, err := reg.GetAgentByName("nemoclaw")
	if err != nil {
		t.Fatalf("lookup by name failed: %v", err)
	}
	if foundByName.ID != agent.ID {
		t.Fatalf("mismatched agent ID from name lookup")
	}

	// Lookup by Token
	foundByToken, err := reg.GetAgentByToken(rawToken)
	if err != nil {
		t.Fatalf("lookup by token failed: %v", err)
	}
	if foundByToken.Name != "nemoclaw" {
		t.Fatalf("mismatched agent name from token lookup")
	}

	// Invalid token lookup
	_, err = reg.GetAgentByToken("clw_agent_invalidtoken999")
	if err == nil {
		t.Fatalf("expected error for invalid token, got nil")
	}

	// Update rules
	err = reg.UpdateAgentRules("nemoclaw", []string{"slack:*", "jira:*"})
	if err != nil {
		t.Fatalf("failed to update rules: %v", err)
	}

	updated, _ := reg.GetAgentByName("nemoclaw")
	if len(updated.AllowedRules) != 2 {
		t.Fatalf("expected 2 updated rules, got %d", len(updated.AllowedRules))
	}
}

