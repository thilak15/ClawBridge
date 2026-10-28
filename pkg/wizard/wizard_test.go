package wizard_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/vault"
	"clawbridge/pkg/wizard"
)

func setupTestVault(t *testing.T) (*vault.CredentialVault, *vault.DatabaseManager, func()) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-wizard-test-*")
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
	cleanup := func() {
		_ = dbMgr.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return v, dbMgr, cleanup
}

func TestRunConnectWizard_Slack(t *testing.T) {
	v, _, cleanup := setupTestVault(t)
	defer cleanup()

	input := "xoxb-test-token-123\nslack-secret-456\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	err := wizard.RunConnectWizard(in, out, v, "slack")
	if err != nil {
		t.Fatalf("unexpected error running slack connect wizard: %v", err)
	}

	if !strings.Contains(out.String(), "Successfully encrypted and stored Slack credentials") {
		t.Errorf("unexpected output: %s", out.String())
	}

	cred, err := v.GetCredentialByChannel("slack")
	if err != nil {
		t.Fatalf("failed to retrieve slack credential: %v", err)
	}
	if cred.Data["token"] != "xoxb-test-token-123" {
		t.Errorf("expected token xoxb-test-token-123, got %v", cred.Data["token"])
	}

	tok, err := v.GetOAuthTokenByChannel("slack")
	if err != nil {
		t.Fatalf("failed to retrieve slack oauth token: %v", err)
	}
	if tok.TokenData["access_token"] != "xoxb-test-token-123" {
		t.Errorf("expected access_token xoxb-test-token-123, got %v", tok.TokenData["access_token"])
	}
}

func TestRunConnectWizard_Jira(t *testing.T) {
	v, _, cleanup := setupTestVault(t)
	defer cleanup()

	input := "https://example.atlassian.net\neng@example.com\nsecret-api-token\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	err := wizard.RunConnectWizard(in, out, v, "jira")
	if err != nil {
		t.Fatalf("unexpected error running jira connect wizard: %v", err)
	}

	if !strings.Contains(out.String(), "Successfully encrypted and stored Jira credentials") {
		t.Errorf("unexpected output: %s", out.String())
	}

	cred, err := v.GetCredentialByChannel("jira")
	if err != nil {
		t.Fatalf("failed to retrieve jira credential: %v", err)
	}
	if cred.Data["email"] != "eng@example.com" || cred.Data["token"] != "secret-api-token" {
		t.Errorf("unexpected jira data: %v", cred.Data)
	}
}

func TestRunConnectWizard_AppMenuSelection(t *testing.T) {
	v, _, cleanup := setupTestVault(t)
	defer cleanup()

	// Select 3 (Discord), then enter token
	input := "3\ndiscord-bot-token-999\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	err := wizard.RunConnectWizard(in, out, v, "")
	if err != nil {
		t.Fatalf("unexpected error running app menu connect wizard: %v", err)
	}

	cred, err := v.GetCredentialByChannel("discord")
	if err != nil {
		t.Fatalf("failed to retrieve discord credential: %v", err)
	}
	if cred.Data["token"] != "discord-bot-token-999" {
		t.Errorf("unexpected discord data: %v", cred.Data)
	}
}

func TestRunConfigWizard_FullAccessAndGranular(t *testing.T) {
	_, dbMgr, cleanup := setupTestVault(t)
	defer cleanup()

	reg := auth.NewAgentRegistry(dbMgr)
	_, _, err := reg.RegisterAgent("nemoclaw", []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = reg.RegisterAgent("openclaw", []string{"*"})
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	_, err = agents.CreateAgentYAML(agentsDir, agents.AgentConfig{
		Name:        "openclaw",
		Permissions: []string{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run wizard for openclaw (selection 2), Granular mode (option 2)
	// Enable Slack: y, Jira: y, Discord: n, Twitter: n, GitHub: n
	input := "openclaw\n2\ny\ny\nn\nn\nn\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	err = wizard.RunConfigWizard(in, out, reg, agentsDir)
	if err != nil {
		t.Fatalf("unexpected error running config wizard: %v", err)
	}

	if !strings.Contains(out.String(), "Updated permissions for 'openclaw'") {
		t.Errorf("unexpected output: %s", out.String())
	}

	// Verify database record
	rec, err := reg.GetAgentByName("openclaw")
	if err != nil {
		t.Fatalf("failed to get agent record: %v", err)
	}
	if len(rec.AllowedRules) != 2 || rec.AllowedRules[0] != "slack:*" || rec.AllowedRules[1] != "jira:*" {
		t.Errorf("unexpected updated rules: %v", rec.AllowedRules)
	}

	// Verify agents/openclaw.yaml file updated
	cfgs, err := agents.LoadAgentsFromDir(agentsDir, nil)
	if err != nil {
		t.Fatalf("failed to load agents from dir: %v", err)
	}
	if len(cfgs) != 1 || len(cfgs[0].Permissions) != 2 {
		t.Errorf("expected updated permissions in openclaw.yaml, got %v", cfgs[0].Permissions)
	}
}
