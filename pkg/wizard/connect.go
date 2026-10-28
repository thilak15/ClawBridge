package wizard

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"clawbridge/pkg/vault"
)

// RunConnectWizard interactively prompts for app credentials and saves them encrypted into the vault.
func RunConnectWizard(in io.Reader, out io.Writer, v *vault.CredentialVault, targetApp string) error {
	if v == nil {
		return fmt.Errorf("vault cannot be nil")
	}

	scanner := bufio.NewScanner(in)
	prompt := func(msg string) string {
		fmt.Fprint(out, msg)
		if !scanner.Scan() {
			return ""
		}
		return strings.TrimSpace(scanner.Text())
	}

	app := strings.ToLower(strings.TrimSpace(targetApp))
	if app == "" {
		fmt.Fprintln(out, "Supported Connectors:")
		fmt.Fprintln(out, "1) Slack")
		fmt.Fprintln(out, "2) Jira")
		fmt.Fprintln(out, "3) Discord")
		fmt.Fprintln(out, "4) Twitter / X")
		fmt.Fprintln(out, "5) GitHub")
		choice := prompt("Select an app to connect [1-5 or name]: ")
		app = normalizeAppName(choice)
	}

	switch app {
	case "slack":
		fmt.Fprintln(out, "Connecting Slack...")
		token := prompt("Slack Bot User OAuth Token (xoxb-...): ")
		if token == "" {
			return fmt.Errorf("Slack bot token cannot be empty")
		}
		secret := prompt("Slack Signing Secret (press enter to skip): ")

		data := map[string]any{
			"token": token,
		}
		if secret != "" {
			data["signing_secret"] = secret
		}

		if _, err := v.UpsertCredentialByChannel("slack", "bearer", data); err != nil {
			return fmt.Errorf("failed to store slack credential: %w", err)
		}
		tokData := map[string]any{"access_token": token}
		if _, err := v.UpsertOAuthTokenByChannel("slack", "default", tokData, 0); err != nil {
			return fmt.Errorf("failed to store slack oauth token: %w", err)
		}
		fmt.Fprintln(out, "Successfully encrypted and stored Slack credentials in vault.")

	case "jira":
		fmt.Fprintln(out, "Connecting Atlassian Jira...")
		baseURL := prompt("Atlassian Jira URL (e.g. https://your-domain.atlassian.net): ")
		email := prompt("Jira Account Email: ")
		if email == "" {
			return fmt.Errorf("Jira email cannot be empty")
		}
		apiToken := prompt("Jira API Token: ")
		if apiToken == "" {
			return fmt.Errorf("Jira API token cannot be empty")
		}

		data := map[string]any{
			"email": email,
			"token": apiToken,
		}
		if baseURL != "" {
			data["base_url"] = baseURL
		}

		if _, err := v.UpsertCredentialByChannel("jira", "basic_auth", data); err != nil {
			return fmt.Errorf("failed to store jira credential: %w", err)
		}
		fmt.Fprintln(out, "Successfully encrypted and stored Jira credentials in vault.")

	case "discord":
		fmt.Fprintln(out, "Connecting Discord...")
		token := prompt("Discord Bot Token: ")
		if token == "" {
			return fmt.Errorf("Discord bot token cannot be empty")
		}

		data := map[string]any{"token": token}
		if _, err := v.UpsertCredentialByChannel("discord", "bot_token", data); err != nil {
			return fmt.Errorf("failed to store discord credential: %w", err)
		}
		fmt.Fprintln(out, "Successfully encrypted and stored Discord credentials in vault.")

	case "twitter", "x":
		fmt.Fprintln(out, "Connecting Twitter / X...")
		token := prompt("Twitter/X Bearer Token: ")
		if token == "" {
			return fmt.Errorf("Twitter bearer token cannot be empty")
		}

		data := map[string]any{"token": token}
		if _, err := v.UpsertCredentialByChannel("twitter", "bearer", data); err != nil {
			return fmt.Errorf("failed to store twitter credential: %w", err)
		}
		fmt.Fprintln(out, "Successfully encrypted and stored Twitter/X credentials in vault.")

	case "github":
		fmt.Fprintln(out, "Connecting GitHub...")
		token := prompt("GitHub Personal Access Token: ")
		if token == "" {
			return fmt.Errorf("GitHub token cannot be empty")
		}

		data := map[string]any{"token": token}
		if _, err := v.UpsertCredentialByChannel("github", "bearer", data); err != nil {
			return fmt.Errorf("failed to store github credential: %w", err)
		}
		fmt.Fprintln(out, "Successfully encrypted and stored GitHub credentials in vault.")

	default:
		return fmt.Errorf("unsupported application: '%s'", app)
	}

	return nil
}

func normalizeAppName(input string) string {
	clean := strings.ToLower(strings.TrimSpace(input))
	switch clean {
	case "1", "slack":
		return "slack"
	case "2", "jira":
		return "jira"
	case "3", "discord":
		return "discord"
	case "4", "twitter", "x":
		return "twitter"
	case "5", "github":
		return "github"
	default:
		return clean
	}
}

