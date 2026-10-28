package wizard

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
)

// RunConfigWizard interactively configures agent permissions in SQLite and updates agents/*.yaml.
func RunConfigWizard(in io.Reader, out io.Writer, registry *auth.AgentRegistry, agentsDir string) error {
	if registry == nil {
		return fmt.Errorf("agent registry cannot be nil")
	}

	scanner := bufio.NewScanner(in)
	prompt := func(msg string) string {
		fmt.Fprint(out, msg)
		if !scanner.Scan() {
			return ""
		}
		return strings.TrimSpace(scanner.Text())
	}

	agentList, err := registry.ListAgents()
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	if len(agentList) == 0 && agentsDir != "" {
		_, _ = agents.LoadAgentsFromDir(agentsDir, registry)
		agentList, _ = registry.ListAgents()
	}

	if len(agentList) == 0 {
		fmt.Fprintln(out, "No registered agents found.")
		return nil
	}

	fmt.Fprintln(out, "Registered Agents:")
	for i, agt := range agentList {
		rulesStr := strings.Join(agt.AllowedRules, ", ")
		if rulesStr == "" {
			rulesStr = "(none)"
		}
		fmt.Fprintf(out, "%d) %s [rules: %s]\n", i+1, agt.Name, rulesStr)
	}

	selection := prompt(fmt.Sprintf("Select agent [1-%d or name]: ", len(agentList)))
	var selectedAgent *auth.AgentRecord

	if idx, err := strconv.Atoi(selection); err == nil && idx >= 1 && idx <= len(agentList) {
		selectedAgent = agentList[idx-1]
	} else {
		for _, agt := range agentList {
			if strings.EqualFold(agt.Name, selection) {
				selectedAgent = agt
				break
			}
		}
	}

	if selectedAgent == nil {
		return fmt.Errorf("invalid agent selection: '%s'", selection)
	}

	fmt.Fprintf(out, "\nConfiguring permissions for '%s':\n", selectedAgent.Name)
	fmt.Fprintln(out, "1) Full Access to All Apps (*)")
	fmt.Fprintln(out, "2) Granular Selection (toggle Slack, Jira, Twitter, Discord, GitHub)")

	mode := prompt("Select mode [1 or 2]: ")

	var newRules []string
	if mode == "1" || mode == "*" || mode == "" {
		newRules = []string{"*"}
	} else if mode == "2" {
		apps := []struct {
			name string
			rule string
		}{
			{"Slack", "slack:*"},
			{"Jira", "jira:*"},
			{"Discord", "discord:*"},
			{"Twitter / X", "twitter:*"},
			{"GitHub", "github:*"},
		}

		for _, item := range apps {
			ans := prompt(fmt.Sprintf("Allow %s access? [y/N]: ", item.name))
			if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
				newRules = append(newRules, item.rule)
			}
		}

		if len(newRules) == 0 {
			fmt.Fprintln(out, "No specific apps enabled. Defaulting to empty access.")
		}
	} else {
		return fmt.Errorf("invalid mode selection: '%s'", mode)
	}

	if err := registry.UpdateAgentRules(selectedAgent.Name, newRules); err != nil {
		return fmt.Errorf("failed to update agent rules in database: %w", err)
	}

	if agentsDir != "" {
		_ = agents.UpdateAgentYAMLPermissions(agentsDir, selectedAgent.Name, newRules)
	}

	rulesDisplay := strings.Join(newRules, ", ")
	if rulesDisplay == "" {
		rulesDisplay = "(none)"
	}
	fmt.Fprintf(out, "Updated permissions for '%s': [%s]\n", selectedAgent.Name, rulesDisplay)
	return nil
}

