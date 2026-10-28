package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbridge/cmd/clawbridge/cli"
)

func TestCLI_VersionAndHelp(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := cli.Execute([]string{"--version"}, nil, out, errOut)
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(out.String(), "ClawBridge v1.0.0") {
		t.Errorf("expected version output, got:\n%s", out.String())
	}

	out.Reset()
	code = cli.Execute([]string{"--help"}, nil, out, errOut)
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(out.String(), "Usage: clawbridge <command>") {
		t.Errorf("expected help output, got:\n%s", out.String())
	}
}

func TestCLI_AgentCreate(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("CLAWBRIDGE_HOME", tempDir)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := cli.Execute([]string{"agent", "create", "workerbot"}, nil, out, errOut)
	if code != 0 {
		t.Fatalf("expected code 0, got %d: %s", code, errOut.String())
	}

	if !strings.Contains(out.String(), "Created agent configuration") {
		t.Errorf("unexpected output: %s", out.String())
	}

	// Verify agent YAML file was created
	agentsDir := filepath.Join(tempDir, "agents")
	workerYAML := filepath.Join(agentsDir, "workerbot.yaml")
	if _, err := os.Stat(workerYAML); err != nil {
		t.Errorf("expected workerbot.yaml to exist: %v", err)
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := cli.Execute([]string{"invalidcmd"}, nil, out, errOut)
	if code != 1 {
		t.Errorf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Unknown command") {
		t.Errorf("expected unknown command error, got: %s", errOut.String())
	}
}
