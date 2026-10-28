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

// GetDefaultNemoClawConfigDir returns the default directory for NVIDIA NemoClaw configuration (~/.nemoclaw).
func GetDefaultNemoClawConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".nemoclaw"), nil
}

// ConfigureNemoClawBridge writes the NVIDIA OpenShell sandbox policy, NeMo Guardrails, and MCP configuration.
func ConfigureNemoClawBridge(configDir string, clawbridgeBin string) (string, error) {
	if configDir == "" {
		var err error
		configDir, err = GetDefaultNemoClawConfigDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create nemoclaw config directory '%s': %w", configDir, err)
	}

	if clawbridgeBin == "" {
		if self, err := os.Executable(); err == nil {
			clawbridgeBin = self
		} else {
			clawbridgeBin = "clawbridge"
		}
	}

	// 1. Write OpenShell sandbox policy: sandbox.yaml
	sandboxContent := fmt.Sprintf(`version: "v1"
metadata:
  name: "nemoclaw-sandboxed"
  description: "NVIDIA OpenShell sandbox policy with ClawBridge Zero-Trust Egress"
security:
  isolation: "landlock-seccomp"
  read_only_root: true
network:
  allow_egress:
    - "127.0.0.1"
    - "localhost"
  deny_direct_internet: true
mcp_servers:
  clawbridge:
    command: "%s"
    args: ["mcp"]
`, clawbridgeBin)

	sandboxPath := filepath.Join(configDir, "sandbox.yaml")
	if err := os.WriteFile(sandboxPath, []byte(sandboxContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write sandbox policy '%s': %w", sandboxPath, err)
	}

	// 2. Write NeMo Guardrails configuration: rails/config.yml
	railsDir := filepath.Join(configDir, "rails")
	_ = os.MkdirAll(railsDir, 0755)
	railsConfig := `models:
  - type: main
    engine: nemotron
rails:
  input:
    flows:
      - check jailbreak
      - check input safety
  output:
    flows:
      - check output safety
      - check sensitive data leakage
`
	railsPath := filepath.Join(railsDir, "config.yml")
	if err := os.WriteFile(railsPath, []byte(railsConfig), 0644); err != nil {
		return "", fmt.Errorf("failed to write rails config '%s': %w", railsPath, err)
	}

	// 3. Write MCP tool definitions: mcp.json
	mcpPath := filepath.Join(configDir, "mcp.json")
	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"clawbridge": map[string]any{
				"command": clawbridgeBin,
				"args":    []string{"mcp"},
			},
		},
	}
	mcpBytes, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err == nil {
		_ = os.WriteFile(mcpPath, mcpBytes, 0644)
	}

	return sandboxPath, nil
}

// DetectNemoClawBinary checks PATH and standard installation locations for the nemoclaw or openshell CLI.
func DetectNemoClawBinary() (string, bool) {
	candidates := []string{"nemoclaw", "openshell"}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	home, _ := os.UserHomeDir()
	pathCandidates := []string{
		filepath.Join(home, ".local", "bin", "nemoclaw"),
		filepath.Join(home, ".local", "bin", "openshell"),
		"/opt/homebrew/bin/nemoclaw",
		"/usr/local/bin/nemoclaw",
		"/usr/local/bin/openshell",
	}
	for _, c := range pathCandidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, true
		}
	}
	return "", false
}

// RunNemoClaw configures the ClawBridge OpenShell bridge and launches the official NVIDIA NemoClaw runtime.
func RunNemoClaw(ctx context.Context, configDir string, clawbridgeBin string, extraArgs []string, envVars map[string]string, in io.Reader, out io.Writer, errOut io.Writer) error {
	policyFile, err := ConfigureNemoClawBridge(configDir, clawbridgeBin)
	if err != nil {
		return fmt.Errorf("nemoclaw bridge setup error: %w", err)
	}
	fmt.Fprintf(out, "Configured NVIDIA OpenShell sandbox policy in '%s'\n", policyFile)

	binPath, found := DetectNemoClawBinary()
	var cmd *exec.Cmd

	if found {
		fmt.Fprintf(out, "Found NVIDIA runtime binary at '%s'. Starting NemoClaw...\n", binPath)
		cmd = exec.CommandContext(ctx, binPath, extraArgs...)
	} else {
		// Check if docker is available to run containerized NemoClaw
		if dockerPath, err := exec.LookPath("docker"); err == nil {
			fmt.Fprintf(out, "NemoClaw binary not in PATH. Running via NVIDIA OpenShell container (%s)...\n", dockerPath)
			dockerArgs := []string{
				"run", "--rm", "-it",
				"-v", fmt.Sprintf("%s:/root/.nemoclaw", filepath.Dir(policyFile)),
				"nvcr.io/nvidia/nemoclaw:latest",
			}
			dockerArgs = append(dockerArgs, extraArgs...)
			cmd = exec.CommandContext(ctx, dockerPath, dockerArgs...)
		} else {
			return fmt.Errorf(
				"official NVIDIA NemoClaw / OpenShell runtime not found. To install it, run:\n" +
					"  curl -fsSL https://www.nvidia.com/nemoclaw.sh | bash\n" +
					"or:\n" +
					"  curl -LsSf https://raw.githubusercontent.com/NVIDIA/OpenShell/main/install.sh | sh",
			)
		}
	}

	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut

	cmdEnv := os.Environ()
	for k, v := range envVars {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmdEnv = append(cmdEnv, fmt.Sprintf("NEMOCLAW_CONFIG_DIR=%s", filepath.Dir(policyFile)))
	cmdEnv = append(cmdEnv, fmt.Sprintf("OPENSHELL_POLICY=%s", policyFile))
	cmd.Env = cmdEnv

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("nemoclaw process exited with error: %w", err)
	}

	return nil
}
