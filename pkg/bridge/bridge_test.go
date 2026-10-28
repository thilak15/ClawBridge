package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureOpenClawBridge(t *testing.T) {
	tempDir := t.TempDir()

	cfgFile, err := ConfigureOpenClawBridge(tempDir, "/mock/bin/clawbridge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	var mcpCfg OpenClawMCPConfig
	if err := json.Unmarshal(data, &mcpCfg); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	entry, ok := mcpCfg.MCPServers["clawbridge"]
	if !ok {
		t.Fatalf("expected 'clawbridge' in mcpServers, got %v", mcpCfg.MCPServers)
	}

	if entry.Command != "/mock/bin/clawbridge" {
		t.Errorf("expected command '/mock/bin/clawbridge', got '%s'", entry.Command)
	}
	if len(entry.Args) != 1 || entry.Args[0] != "mcp" {
		t.Errorf("expected args ['mcp'], got %v", entry.Args)
	}
}

func TestConfigureNemoClawBridge(t *testing.T) {
	tempDir := t.TempDir()

	policyFile, err := ConfigureNemoClawBridge(tempDir, "/mock/bin/clawbridge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify sandbox.yaml
	sandboxData, err := os.ReadFile(policyFile)
	if err != nil {
		t.Fatalf("failed to read sandbox policy: %v", err)
	}
	if len(sandboxData) == 0 {
		t.Fatal("sandbox policy is empty")
	}

	// Verify NeMo Guardrails
	railsPath := filepath.Join(tempDir, "rails", "config.yml")
	railsData, err := os.ReadFile(railsPath)
	if err != nil {
		t.Fatalf("failed to read rails config: %v", err)
	}
	if len(railsData) == 0 {
		t.Fatal("rails config is empty")
	}

	// Verify mcp.json
	mcpPath := filepath.Join(tempDir, "mcp.json")
	mcpData, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("failed to read mcp.json: %v", err)
	}
	var mcpMap map[string]any
	if err := json.Unmarshal(mcpData, &mcpMap); err != nil {
		t.Fatalf("failed to parse mcp.json: %v", err)
	}
	if _, ok := mcpMap["mcpServers"]; !ok {
		t.Fatal("mcpServers not found in mcp.json")
	}
}

func TestDetectBinaries(t *testing.T) {
	// Should not crash even when binaries are not in PATH
	_, _ = DetectOpenClawBinary()
	_, _ = DetectNemoClawBinary()
}
