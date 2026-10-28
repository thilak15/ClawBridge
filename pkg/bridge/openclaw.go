package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// OpenClawMCPConfig defines the Model Context Protocol configuration structure for OpenClaw.
type OpenClawMCPConfig struct {
	MCPServers map[string]MCPServerEntry `json:"mcpServers"`
}

// MCPServerEntry represents a single MCP server invocation definition.
type MCPServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// GetDefaultOpenClawConfigDir returns the default directory for OpenClaw configuration (~/.openclaw).
func GetDefaultOpenClawConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

// ConfigureOpenClawBridge creates or updates openclaw.json with the ClawBridge MCP server definition.
func ConfigureOpenClawBridge(configDir string, clawbridgeBin string) (string, error) {
	if configDir == "" {
		var err error
		configDir, err = GetDefaultOpenClawConfigDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create openclaw config directory '%s': %w", configDir, err)
	}

	if clawbridgeBin == "" {
		if self, err := os.Executable(); err == nil {
			clawbridgeBin = self
		} else {
			clawbridgeBin = "clawbridge"
		}
	}

	cfgPath := filepath.Join(configDir, "openclaw.json")
	var existing OpenClawMCPConfig
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing.MCPServers == nil {
		existing.MCPServers = make(map[string]MCPServerEntry)
	}

	existing.MCPServers["clawbridge"] = MCPServerEntry{
		Command: clawbridgeBin,
		Args:    []string{"mcp"},
	}

	outBytes, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal openclaw mcp config: %w", err)
	}

	if err := os.WriteFile(cfgPath, outBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write '%s': %w", cfgPath, err)
	}

	return cfgPath, nil
}

// DetectOpenClawBinary checks PATH and standard installation locations for the openclaw CLI.
func DetectOpenClawBinary() (string, bool) {
	if path, err := exec.LookPath("openclaw"); err == nil {
		return path, true
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "openclaw"),
		"/opt/homebrew/bin/openclaw",
		"/usr/local/bin/openclaw",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, true
		}
	}
	return "", false
}

// RunOpenClaw configures the ClawBridge MCP bridge and launches the official OpenClaw runtime.
func RunOpenClaw(ctx context.Context, configDir string, clawbridgeBin string, extraArgs []string, envVars map[string]string, in io.Reader, out io.Writer, errOut io.Writer) error {
	cfgFile, err := ConfigureOpenClawBridge(configDir, clawbridgeBin)
	if err != nil {
		return fmt.Errorf("openclaw bridge setup error: %w", err)
	}
	fmt.Fprintf(out, "Configured ClawBridge MCP integration in '%s'\n", cfgFile)

	binPath, found := DetectOpenClawBinary()
	if !found {
		return fmt.Errorf(
			"official OpenClaw runtime ('openclaw') not found in PATH.\n" +
				"To install it, run:\n" +
				"  npm install -g openclaw\n" +
				"or:\n" +
				"  curl -fsSL https://openclaw.ai/install.sh | bash",
		)
	}

	fmt.Fprintf(out, "Found OpenClaw binary at '%s'. Starting OpenClaw...\n", binPath)
	cmd := exec.CommandContext(ctx, binPath, extraArgs...)

	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut

	// Inherit system environment, append/override custom variables
	cmdEnv := os.Environ()
	for k, v := range envVars {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmdEnv = append(cmdEnv, fmt.Sprintf("OPENCLAW_CONFIG_DIR=%s", filepath.Dir(cfgFile)))
	cmd.Env = cmdEnv

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("openclaw process exited with error: %w", err)
	}

	return nil
}
