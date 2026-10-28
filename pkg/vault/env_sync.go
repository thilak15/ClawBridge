package vault

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseEnv parses key-value pairs from an io.Reader in .env format.
func ParseEnv(r io.Reader) (map[string]string, error) {
	result := make(map[string]string)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip optional quotes
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			if len(val) >= 2 {
				val = val[1 : len(val)-1]
			}
		}

		if key != "" {
			result[key] = val
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env data: %w", err)
	}

	return result, nil
}

// LoadEnvFile reads a .env file and returns parsed key-value pairs.
func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseEnv(f)
}

// GetSystemEnv returns all system environment variables as a map.
func GetSystemEnv() map[string]string {
	result := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// SyncEnvVars syncs supported environment secrets into the CredentialVault.
// Returns list of synced channel names.
func SyncEnvVars(v *CredentialVault, envVars map[string]string) ([]string, error) {
	if v == nil || envVars == nil {
		return nil, nil
	}

	var synced []string

	// 1. Slack
	slackToken := strings.TrimSpace(envVars["SLACK_BOT_TOKEN"])
	slackSecret := strings.TrimSpace(envVars["SLACK_SIGNING_SECRET"])
	if slackToken != "" {
		data := map[string]any{
			"token": slackToken,
		}
		if slackSecret != "" {
			data["signing_secret"] = slackSecret
		}
		if _, err := v.UpsertCredentialByChannel("slack", "bearer", data); err != nil {
			return nil, fmt.Errorf("failed to sync slack credential: %w", err)
		}
		tokData := map[string]any{"access_token": slackToken}
		if _, err := v.UpsertOAuthTokenByChannel("slack", "default", tokData, 0); err != nil {
			return nil, fmt.Errorf("failed to sync slack oauth token: %w", err)
		}
		synced = append(synced, "slack")
	}

	// 2. Jira
	jiraURL := strings.TrimSpace(envVars["JIRA_BASE_URL"])
	jiraEmail := strings.TrimSpace(envVars["JIRA_EMAIL"])
	jiraToken := strings.TrimSpace(envVars["JIRA_API_TOKEN"])
	if jiraEmail != "" && jiraToken != "" {
		data := map[string]any{
			"email": jiraEmail,
			"token": jiraToken,
		}
		if jiraURL != "" {
			data["base_url"] = jiraURL
		}
		if _, err := v.UpsertCredentialByChannel("jira", "basic_auth", data); err != nil {
			return nil, fmt.Errorf("failed to sync jira credential: %w", err)
		}
		synced = append(synced, "jira")
	}

	// 3. Discord
	discordToken := strings.TrimSpace(envVars["DISCORD_BOT_TOKEN"])
	if discordToken != "" {
		data := map[string]any{
			"token": discordToken,
		}
		if _, err := v.UpsertCredentialByChannel("discord", "bot_token", data); err != nil {
			return nil, fmt.Errorf("failed to sync discord credential: %w", err)
		}
		synced = append(synced, "discord")
	}

	// 4. Twitter / X
	twitterBearer := strings.TrimSpace(envVars["TWITTER_BEARER_TOKEN"])
	twitterKey := strings.TrimSpace(envVars["TWITTER_API_KEY"])
	twitterSecret := strings.TrimSpace(envVars["TWITTER_API_SECRET"])
	if twitterBearer != "" {
		data := map[string]any{
			"token": twitterBearer,
		}
		if twitterKey != "" {
			data["api_key"] = twitterKey
		}
		if twitterSecret != "" {
			data["api_secret"] = twitterSecret
		}
		if _, err := v.UpsertCredentialByChannel("twitter", "bearer", data); err != nil {
			return nil, fmt.Errorf("failed to sync twitter credential: %w", err)
		}
		synced = append(synced, "twitter")
	} else if twitterKey != "" {
		data := map[string]any{
			"key":    twitterKey,
			"secret": twitterSecret,
		}
		if _, err := v.UpsertCredentialByChannel("twitter", "api_key", data); err != nil {
			return nil, fmt.Errorf("failed to sync twitter api_key: %w", err)
		}
		synced = append(synced, "twitter")
	}

	// 5. GitHub
	githubToken := strings.TrimSpace(envVars["GITHUB_TOKEN"])
	if githubToken != "" {
		data := map[string]any{
			"token": githubToken,
		}
		if _, err := v.UpsertCredentialByChannel("github", "bearer", data); err != nil {
			return nil, fmt.Errorf("failed to sync github credential: %w", err)
		}
		synced = append(synced, "github")
	}

	return synced, nil
}

// AutoSyncEnv detects .env files or system environment variables and encrypts them into the vault.
func AutoSyncEnv(v *CredentialVault, envFilePath string) ([]string, error) {
	combined := make(map[string]string)

	for k, val := range GetSystemEnv() {
		combined[k] = val
	}

	path := envFilePath
	if path == "" {
		if _, err := os.Stat(".env"); err == nil {
			path = ".env"
		}
	}

	if path != "" {
		if fileVars, err := LoadEnvFile(path); err == nil {
			for k, val := range fileVars {
				combined[k] = val
			}
		}
	}

	return SyncEnvVars(v, combined)
}
