package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/bridge"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/vault"
	"clawbridge/pkg/wizard"
)

// ConsoleConfig encapsulates the dependencies needed by the interactive console.
type ConsoleConfig struct {
	In           io.Reader
	Out          io.Writer
	Orchestrator *Orchestrator
	Broadcaster  *inbound.EventBroadcaster
	Vault        *vault.CredentialVault
	Registry     *auth.AgentRegistry
	AgentsDir    string
	Tools        *mcp.Tools
}

// RunConsole runs the hybrid interactive CLI and reactive inbound webhook listener.
func RunConsole(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	orch *Orchestrator,
	broadcaster *inbound.EventBroadcaster,
) error {
	return RunConsoleWithConfig(ctx, ConsoleConfig{
		In:           in,
		Out:          out,
		Orchestrator: orch,
		Broadcaster:  broadcaster,
	})
}

// RunConsoleWithConfig runs the hybrid interactive CLI, management REPL, and reactive inbound webhook listener.
func RunConsoleWithConfig(ctx context.Context, cfg ConsoleConfig) error {
	if cfg.Orchestrator == nil {
		return fmt.Errorf("orchestrator cannot be nil")
	}

	in := cfg.In
	out := cfg.Out
	orch := cfg.Orchestrator
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
					routeInboundEvent(orch, event)
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
	fmt.Fprintln(out, "ClawBridge Multi-Agent Console (Type 'help' for commands, 'exit' to quit)")

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

		case lowerLine == "list":
			running := orch.ListRunning()
			if len(running) == 0 {
				fmt.Fprintln(out, "No agents are currently running.")
			} else {
				fmt.Fprintf(out, "Running agents (%d): %s\n", len(running), strings.Join(running, ", "))
			}

		case lowerLine == "status":
			running := orch.ListRunning()
			if len(running) == 0 {
				fmt.Fprintln(out, "No agents are currently running.")
			} else {
				fmt.Fprintln(out, "Agent Status:")
				for _, name := range running {
					mem := orch.GetAgentMemory(name)
					turns := 0
					if mem != nil {
						turns = mem.Count()
					}
					fmt.Fprintf(out, "  - %s: Active (Sliding memory turns: %d/15)\n", name, turns)
				}
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

		case strings.HasPrefix(lowerLine, "agent create ") || strings.HasPrefix(lowerLine, "create agent "):
			if cfg.Registry == nil || cfg.AgentsDir == "" {
				fmt.Fprintln(out, "Agent registry not available in this session.")
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 3 {
				fmt.Fprintln(out, "Usage: agent create <name>")
				continue
			}
			agentName := strings.ToLower(strings.TrimSpace(parts[2]))
			createdPath, err := agents.CreateAgentYAML(cfg.AgentsDir, agents.AgentConfig{
				Name:         agentName,
				Description:  fmt.Sprintf("Autonomous agent '%s' created via console", agentName),
				SystemPrompt: fmt.Sprintf("You are %s, an autonomous multi-platform agent equipped with ClawBridge tools.", agentName),
				Model:        "gemini-3.8-flash",
				Permissions:  []string{"*"},
			})
			if err != nil {
				fmt.Fprintf(out, "Failed to create agent configuration: %v\n", err)
				continue
			}
			if _, _, err := cfg.Registry.RegisterAgent(agentName, []string{"*"}); err != nil {
				fmt.Fprintf(out, "Failed to register agent in vault: %v\n", err)
				continue
			}
			fmt.Fprintf(out, "Created agent '%s' configuration at %s\n", agentName, createdPath)

			// Dynamically start agent in active orchestrator
			if err := orch.StartAgent(ctx, agentName); err != nil {
				fmt.Fprintf(out, "Warning: agent created but failed to start: %v\n", err)
			} else {
				fmt.Fprintf(out, "Agent '%s' is now running and ready for instructions.\n", agentName)
			}

		case strings.HasPrefix(lowerLine, "run ") || strings.HasPrefix(lowerLine, "start "):
			parts := strings.Fields(line)
			if len(parts) < 2 {
				fmt.Fprintln(out, "Usage: run <openclaw|nemoclaw|agent_name>")
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
			if orch.IsRunning(agentName) {
				fmt.Fprintf(out, "Agent '%s' is already running.\n", agentName)
				continue
			}
			if err := orch.StartAgent(ctx, agentName); err != nil {
				fmt.Fprintf(out, "Failed to start agent '%s': %v\n", agentName, err)
			} else {
				fmt.Fprintf(out, "Agent '%s' is now running.\n", agentName)
			}

		default:
			// Parse <target>: <task>
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				fmt.Fprintf(out, "Unknown command: '%s'. Use '<agent>: <task>', 'connect', 'config', 'agent create <name>', or type 'help'.\n", line)
				continue
			}

			target := strings.TrimSpace(parts[0])
			task := strings.TrimSpace(parts[1])

			if task == "" {
				fmt.Fprintln(out, "Task instruction cannot be empty.")
				continue
			}

			if strings.EqualFold(target, "broadcast") {
				if err := orch.BroadcastTask(task); err != nil {
					fmt.Fprintf(out, "Broadcast failed: %v\n", err)
				} else {
					fmt.Fprintln(out, "Task broadcasted to all running agents.")
				}
			} else {
				if err := orch.DispatchTask(target, task); err != nil {
					fmt.Fprintf(out, "Dispatch failed: %v\n", err)
				} else {
					fmt.Fprintf(out, "Task dispatched to '%s'.\n", target)
				}
			}
		}
	}

	return scanner.Err()
}

func routeInboundEvent(orch *Orchestrator, event *inbound.InboundEvent) {
	payloadStr := event.Content
	if payloadStr == "" && event.Data != nil {
		b, _ := json.Marshal(event.Data)
		payloadStr = string(b)
	}
	taskPrompt := fmt.Sprintf("Inbound event received on topic '%s': %s. React appropriately.", event.Topic, payloadStr)

	// Targeted routing by channel topic
	topicLower := strings.ToLower(event.Topic)
	if strings.HasPrefix(topicLower, "slack:") && orch.IsRunning("nemoclaw") {
		_ = orch.DispatchTask("nemoclaw", taskPrompt)
		return
	}
	if strings.HasPrefix(topicLower, "jira:") && orch.IsRunning("openclaw") {
		_ = orch.DispatchTask("openclaw", taskPrompt)
		return
	}

	// Default fallback: broadcast to all running agents
	_ = orch.BroadcastTask(taskPrompt)
}
