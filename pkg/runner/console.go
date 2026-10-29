package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"clawbridge/pkg/auth"
	"clawbridge/pkg/bridge"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/vault"
	"clawbridge/pkg/wizard"
)

// ConsoleConfig encapsulates the dependencies needed by the interactive console.
type ConsoleConfig struct {
	In          io.Reader
	Out         io.Writer
	Broadcaster *inbound.EventBroadcaster
	Vault       *vault.CredentialVault
	Registry    *auth.AgentRegistry
	AgentsDir   string
	Tools       *mcp.Tools
}

// RunConsole runs the Universal Connector and MCP Gateway interactive console.
func RunConsole(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	broadcaster *inbound.EventBroadcaster,
	v *vault.CredentialVault,
	reg *auth.AgentRegistry,
	tools *mcp.Tools,
) error {
	return RunConsoleWithConfig(ctx, ConsoleConfig{
		In:          in,
		Out:         out,
		Broadcaster: broadcaster,
		Vault:       v,
		Registry:    reg,
		Tools:       tools,
	})
}

// RunConsoleWithConfig runs the hybrid interactive CLI, connector management, and MCP verification console.
func RunConsoleWithConfig(ctx context.Context, cfg ConsoleConfig) error {
	in := cfg.In
	out := cfg.Out
	broadcaster := cfg.Broadcaster

	// 1. Start reactive inbound event listener if broadcaster is provided
	if broadcaster != nil {
		eventChan := broadcaster.Subscribe([]string{"*"})
		go func() {
			defer broadcaster.Unsubscribe(eventChan)
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-eventChan:
					if !ok {
						return
					}
					fmt.Fprintf(out, "\n[Inbound Event: %s] %s (Topic: %s)\nclawbridge> ", event.Channel, event.EventID, event.Topic)
				}
			}
		}()
	}

	// 2. Interactive Terminal REPL loop
	scanner := bufio.NewScanner(in)
	eof := false
	prompt := func(msg string) string {
		fmt.Fprint(out, msg)
		if !scanner.Scan() {
			eof = true
			return ""
		}
		return strings.TrimSpace(scanner.Text())
	}
	fmt.Fprintln(out, "ClawBridge Universal Gateway Console (Type 'help' for commands, 'exit' to quit)")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		rawLine := prompt("clawbridge> ")
		if eof {
			break
		}
		if rawLine == "" {
			continue
		}

		// Strip leading "clawbridge " if user typed it out of habit
		line := rawLine
		if strings.HasPrefix(strings.ToLower(line), "clawbridge ") {
			line = strings.TrimSpace(line[len("clawbridge "):])
		}

		lowerLine := strings.ToLower(line)

		switch {
		case lowerLine == "exit" || lowerLine == "quit":
			return nil

		case lowerLine == "clear":
			fmt.Fprint(out, "\033[H\033[2J")

		case lowerLine == "help":
			fmt.Fprintln(out, "Available Commands:")
			fmt.Fprintln(out, "  Universal Connector & MCP Tools:")
			fmt.Fprintln(out, "    tools                    - List all active Model Context Protocol (MCP) tools")
			fmt.Fprintln(out, "    vault                    - Inspect encrypted credentials and token refresh state")
			fmt.Fprintln(out, "    connect [app]            - Connect platforms (Slack, Jira, GitHub, Google, Meta, etc.)")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Official Upstream Claws Bridge:")
			fmt.Fprintln(out, "    bridge openclaw          - Configure OpenClaw MCP integration (~/.openclaw/openclaw.json)")
			fmt.Fprintln(out, "    bridge nemoclaw          - Configure NVIDIA NemoClaw sandbox (~/.nemoclaw/sandbox.yaml)")
			fmt.Fprintln(out, "    run openclaw [args...]   - Launch official OpenClaw with ClawBridge MCP tools")
			fmt.Fprintln(out, "    run nemoclaw [args...]   - Launch official NVIDIA NemoClaw / OpenShell stack")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Console Controls:")
			fmt.Fprintln(out, "    clear                    - Clear terminal console screen")
			fmt.Fprintln(out, "    help                     - Show this guidance message")
			fmt.Fprintln(out, "    exit / quit              - Leave the console")

		case lowerLine == "tools":
			if cfg.Tools == nil {
				fmt.Fprintln(out, "MCP tool registry not available in this session.")
				continue
			}
			toolList := cfg.Tools.ListTools()
			fmt.Fprintf(out, "Active Model Context Protocol (MCP) Tools (%d):\n", len(toolList))
			for _, t := range toolList {
				fmt.Fprintf(out, "  - %-26s %s\n", t.Name, t.Description)
			}

		case lowerLine == "vault":
			if cfg.Vault == nil {
				fmt.Fprintln(out, "Encrypted vault not available in this session.")
				continue
			}
			fmt.Fprintln(out, "ClawBridge Encrypted Vault (SQLite WAL, AES-256-GCM):")
			channels := []string{"slack", "jira", "discord", "twitter", "github", "telegram", "whatsapp", "meta", "google", "gemini", "openai", "anthropic"}
			connected := 0
			for _, ch := range channels {
				if cred, err := cfg.Vault.GetCredentialByChannel(ch); err == nil && cred != nil {
					fmt.Fprintf(out, "  - %-12s Connected (ID: %s)\n", ch, cred.ID)
					connected++
				}
			}
			if connected == 0 {
				fmt.Fprintln(out, "  No platforms connected yet. Type 'connect' to connect your first platform.")
			}

		case strings.HasPrefix(lowerLine, "connect"):
			if cfg.Vault == nil {
				fmt.Fprintln(out, "Credential vault not available in this session.")
				continue
			}
			parts := strings.Fields(line)
			targetApp := ""
			if len(parts) > 1 {
				targetApp = parts[1]
			}
			if err := wizard.RunConnectWizardWithPrompt(prompt, out, cfg.Vault, targetApp); err != nil {
				fmt.Fprintf(out, "Connect wizard error: %v\n", err)
			}

		case lowerLine == "config":
			if cfg.Registry == nil || cfg.AgentsDir == "" {
				fmt.Fprintln(out, "Agent configuration not available in this session.")
				continue
			}
			if err := wizard.RunConfigWizardWithPrompt(prompt, out, cfg.Registry, cfg.AgentsDir); err != nil {
				fmt.Fprintf(out, "Config wizard error: %v\n", err)
			}

		case strings.HasPrefix(lowerLine, "bridge "):
			parts := strings.Fields(line)
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: bridge <openclaw|nemoclaw>")
				continue
			}
			target := strings.ToLower(parts[1])
			selfBin, _ := os.Executable()
			switch target {
			case "openclaw":
				cfgFile, err := bridge.ConfigureOpenClawBridge("", selfBin)
				if err != nil {
					fmt.Fprintf(out, "OpenClaw bridge error: %v\n", err)
				} else {
					fmt.Fprintf(out, "Successfully configured OpenClaw MCP bridge at '%s'.\n", cfgFile)
				}
			case "nemoclaw":
				policyFile, err := bridge.ConfigureNemoClawBridge("", selfBin)
				if err != nil {
					fmt.Fprintf(out, "NVIDIA NemoClaw bridge error: %v\n", err)
				} else {
					fmt.Fprintf(out, "Successfully configured NVIDIA OpenShell sandbox policy at '%s'.\n", policyFile)
				}
			default:
				fmt.Fprintf(out, "Unknown bridge target: '%s'. Use 'bridge openclaw' or 'bridge nemoclaw'.\n", target)
			}

		case strings.HasPrefix(lowerLine, "run ") || strings.HasPrefix(lowerLine, "start "):
			parts := strings.Fields(line)
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: run <openclaw|nemoclaw> [args...]")
				continue
			}
			agentName := strings.ToLower(strings.TrimSpace(parts[1]))
			selfBin, _ := os.Executable()
			if agentName == "openclaw" {
				if err := bridge.RunOpenClaw(ctx, "", selfBin, parts[2:], nil, in, out, out); err != nil {
					fmt.Fprintf(out, "OpenClaw run error: %v\n", err)
				}
				continue
			}
			if agentName == "nemoclaw" {
				if err := bridge.RunNemoClaw(ctx, "", selfBin, parts[2:], nil, in, out, out); err != nil {
					fmt.Fprintf(out, "NemoClaw run error: %v\n", err)
				}
				continue
			}
			fmt.Fprintf(out, "Unknown run target '%s'. Use 'run openclaw' or 'run nemoclaw'.\n", agentName)

		default:
			fmt.Fprintf(out, "Unknown command: '%s'. Type 'help' for available commands.\n", line)
		}
	}

	return nil
}
