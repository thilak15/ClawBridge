package runner_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbridge/pkg/auth"
	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/runner"
	"clawbridge/pkg/vault"
)

func setupTestEnvironment(t *testing.T) (*auth.AgentRegistry, *vault.CredentialVault, *mcp.Tools, string, func()) {
	tmpDir, err := os.MkdirTemp("", "clawbridge-runner-test-*")
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "vault.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = dbMgr.Initialize()

	reg := auth.NewAgentRegistry(dbMgr)
	agentsDir := filepath.Join(tmpDir, "agents")
	_ = os.MkdirAll(agentsDir, 0755)

	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	credVault := vault.NewCredentialVault(dbMgr, cipher)
	limiter := dispatch.NewTokenBucketLimiter(nil)
	dispatcher := dispatch.NewEgressDispatcher(credVault, limiter, nil)
	tools := mcp.NewTools(dispatcher, nil, nil)

	cleanup := func() {
		_ = dbMgr.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return reg, credVault, tools, agentsDir, cleanup
}

func TestConsole_UniversalConnectorCommands(t *testing.T) {
	reg, credVault, tools, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	inputCommands := "tools\nvault\nbridge openclaw\nbridge nemoclaw\nhelp\nexit\n"
	inBuf := strings.NewReader(inputCommands)
	outBuf := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runner.RunConsoleWithConfig(ctx, runner.ConsoleConfig{
		In:        inBuf,
		Out:       outBuf,
		Vault:     credVault,
		Registry:  reg,
		AgentsDir: agentsDir,
		Tools:     tools,
	})
	if err != nil {
		t.Fatalf("console returned error: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "Active Model Context Protocol (MCP) Tools") {
		t.Errorf("expected MCP tools output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "ClawBridge Encrypted Vault") {
		t.Errorf("expected vault status output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Successfully configured OpenClaw MCP bridge") {
		t.Errorf("expected OpenClaw bridge output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Successfully configured NVIDIA OpenShell sandbox policy") {
		t.Errorf("expected NVIDIA NemoClaw bridge output, got:\n%s", outStr)
	}
}

func TestConsole_InboundEventReaction(t *testing.T) {
	reg, credVault, tools, agentsDir, cleanup := setupTestEnvironment(t)
	defer cleanup()

	broadcaster := inbound.NewEventBroadcaster()
	inBuf := strings.NewReader("exit\n")
	outBuf := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		broadcaster.Publish(&inbound.InboundEvent{
			EventID: "evt_123",
			Channel: "slack",
			Topic:   "channel.message",
		})
	}()

	err := runner.RunConsoleWithConfig(ctx, runner.ConsoleConfig{
		In:          inBuf,
		Out:         outBuf,
		Broadcaster: broadcaster,
		Vault:       credVault,
		Registry:    reg,
		AgentsDir:   agentsDir,
		Tools:       tools,
	})
	if err != nil {
		t.Fatalf("console returned error: %v", err)
	}
}
