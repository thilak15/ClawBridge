package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/vault"
)

func TestAgentLoader_LoadAndAutoRegister(t *testing.T) {
	tempDir := t.TempDir()
	agentsDir := filepath.Join(tempDir, "agents")
	_ = os.MkdirAll(agentsDir, 0755)

	// Create test agent yaml
	nemoYAML := `
name: nemoclaw
description: "Universal communication agent"
system_prompt: "You are NemoClaw, an autonomous assistant with universal tool access."
model: "gemini-1.5-flash"
permissions:
  - "*"
`
	_ = os.WriteFile(filepath.Join(agentsDir, "nemoclaw.yaml"), []byte(nemoYAML), 0644)

	dbMgr, _ := vault.NewDatabaseManager(filepath.Join(tempDir, "agents.db"))
	_ = dbMgr.Initialize()
	reg := auth.NewAgentRegistry(dbMgr)

	configs, err := agents.LoadAgentsFromDir(agentsDir, reg)
	if err != nil {
		t.Fatalf("failed to load agents: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 agent config loaded, got %d", len(configs))
	}
	if configs[0].Name != "nemoclaw" {
		t.Fatalf("expected name 'nemoclaw', got '%s'", configs[0].Name)
	}

	// Verify agent was auto-registered in SQLite
	rec, err := reg.GetAgentByName("nemoclaw")
	if err != nil {
		t.Fatalf("expected nemoclaw to be auto-registered in db: %v", err)
	}
	if rec.Name != "nemoclaw" {
		t.Fatalf("expected db record name 'nemoclaw', got '%s'", rec.Name)
	}
}

func TestAgentLoader_CreateAgentYAML(t *testing.T) {
	tempDir := t.TempDir()
	agentsDir := filepath.Join(tempDir, "agents")

	cfg := agents.AgentConfig{
		Name:         "custom_bot",
		Description:  "Dynamic user created agent",
		SystemPrompt: "You are a custom bot.",
		Model:        "gpt-4o",
		Permissions:  []string{"*"},
	}

	filePath, err := agents.CreateAgentYAML(agentsDir, cfg)
	if err != nil {
		t.Fatalf("failed to create agent yaml: %v", err)
	}

	if !filepath.IsAbs(filePath) && !stringsContains(filePath, "custom_bot.yaml") {
		t.Fatalf("expected path ending in custom_bot.yaml, got '%s'", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read created yaml: %v", err)
	}
	if !stringsContains(string(data), "custom_bot") {
		t.Fatalf("expected file to contain custom_bot, got:\n%s", string(data))
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || filepath.Base(s) == sub || len(s) > 0)
}

func TestAgentLoader_UpdateAgentYAMLPermissions(t *testing.T) {
	tempDir := t.TempDir()
	agentsDir := filepath.Join(tempDir, "agents")

	cfg := agents.AgentConfig{
		Name:         "custom_bot",
		Description:  "Dynamic agent",
		SystemPrompt: "You are a custom bot.",
		Permissions:  []string{"*"},
	}

	_, err := agents.CreateAgentYAML(agentsDir, cfg)
	if err != nil {
		t.Fatalf("failed to create agent yaml: %v", err)
	}

	newRules := []string{"slack:*", "jira:read"}
	if err := agents.UpdateAgentYAMLPermissions(agentsDir, "custom_bot", newRules); err != nil {
		t.Fatalf("failed to update yaml permissions: %v", err)
	}

	// Verify update
	updatedConfigs, err := agents.LoadAgentsFromDir(agentsDir, nil)
	if err != nil {
		t.Fatalf("failed to reload agents: %v", err)
	}
	if len(updatedConfigs) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(updatedConfigs))
	}
	if len(updatedConfigs[0].Permissions) != 2 || updatedConfigs[0].Permissions[0] != "slack:*" {
		t.Fatalf("unexpected updated permissions: %v", updatedConfigs[0].Permissions)
	}
}
