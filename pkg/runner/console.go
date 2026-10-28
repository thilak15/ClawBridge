package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"clawbridge/pkg/inbound"
)

// RunConsole runs the hybrid interactive CLI and reactive inbound webhook listener.
func RunConsole(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	orch *Orchestrator,
	broadcaster *inbound.EventBroadcaster,
) error {
	if orch == nil {
		return fmt.Errorf("orchestrator cannot be nil")
	}

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
	fmt.Fprintln(out, "ClawBridge Multi-Agent Console (Type 'help' for commands, 'exit' to quit)")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		fmt.Fprint(out, "clawbridge> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch strings.ToLower(line) {
		case "exit", "quit":
			return nil

		case "help":
			fmt.Fprintln(out, "Available Commands:")
			fmt.Fprintln(out, "  <agent_name>: <task>     - Send a direct instruction to an agent")
			fmt.Fprintln(out, "  broadcast: <task>        - Send an instruction to all running agents")
			fmt.Fprintln(out, "  list                     - List all running agents")
			fmt.Fprintln(out, "  status                   - Show agent turn counts and active memory")
			fmt.Fprintln(out, "  help                     - Show this guidance message")
			fmt.Fprintln(out, "  exit / quit              - Leave the console")

		case "list":
			agents := orch.ListRunning()
			if len(agents) == 0 {
				fmt.Fprintln(out, "No agents are currently running.")
			} else {
				fmt.Fprintf(out, "Running agents (%d): %s\n", len(agents), strings.Join(agents, ", "))
			}

		case "status":
			agents := orch.ListRunning()
			if len(agents) == 0 {
				fmt.Fprintln(out, "No agents are currently running.")
			} else {
				fmt.Fprintln(out, "Agent Status:")
				for _, name := range agents {
					mem := orch.GetAgentMemory(name)
					turns := 0
					if mem != nil {
						turns = mem.Count()
					}
					fmt.Fprintf(out, "  - %s: Active (Sliding memory turns: %d/15)\n", name, turns)
				}
			}

		default:
			// Parse <target>: <task>
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				fmt.Fprintln(out, "Unknown command format. Use '<agent>: <task>' or 'broadcast: <task>'. Type 'help' for details.")
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
