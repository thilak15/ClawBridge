package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clawbridge/pkg/dispatch"
)

// ToolDefinition defines the metadata and input schema for a Model Context Protocol tool.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Tools encapsulates normalized tool handlers dispatched through the EgressDispatcher.
type Tools struct {
	dispatcher    *dispatch.EgressDispatcher
	defaultTokens map[string]string
	defaultCreds  map[string]string
}

// NewTools creates a new Tools instance.
func NewTools(
	dispatcher *dispatch.EgressDispatcher,
	defaultTokens map[string]string,
	defaultCreds map[string]string,
) *Tools {
	if defaultTokens == nil {
		defaultTokens = make(map[string]string)
	}
	if defaultCreds == nil {
		defaultCreds = make(map[string]string)
	}
	return &Tools{
		dispatcher:    dispatcher,
		defaultTokens: defaultTokens,
		defaultCreds:  defaultCreds,
	}
}

// ListTools returns the definitions for all available ClawBridge MCP tools.
func (t *Tools) ListTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "slack_post_message",
			Description: "Post a chat message to a Slack channel using an authenticated agent bearer token.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{"type": "string", "description": "The Slack channel ID (e.g. C12345678)"},
					"text":       map[string]any{"type": "string", "description": "The text message to send"},
					"token_id":   map[string]any{"type": "string", "description": "Optional token ID in Vault (defaults to channel default)"},
				},
				"required": []string{"channel_id", "text"},
			},
		},
		{
			Name:        "jira_create_issue",
			Description: "Create a Jira issue in a given project using an authenticated agent bearer token.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key":   map[string]any{"type": "string", "description": "The Jira project key (e.g. PROJ)"},
					"summary":       map[string]any{"type": "string", "description": "Issue summary/title"},
					"description":   map[string]any{"type": "string", "description": "Detailed issue description"},
					"issue_type":    map[string]any{"type": "string", "description": "Issue type (Task, Bug, Story). Default Task"},
					"credential_id": map[string]any{"type": "string", "description": "Optional credential ID in Vault"},
					"jira_base_url": map[string]any{"type": "string", "description": "Atlassian base URL (e.g. https://your-domain.atlassian.net)"},
				},
				"required": []string{"project_key", "summary"},
			},
		},
		{
			Name:        "twitter_post_tweet",
			Description: "Post a tweet (X post) on behalf of an authenticated account using an agent bearer token.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":     map[string]any{"type": "string", "description": "The tweet body text"},
					"token_id": map[string]any{"type": "string", "description": "Optional token ID in Vault"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "discord_send_message",
			Description: "Send a message to a Discord channel using an authenticated agent bearer token.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id":    map[string]any{"type": "string", "description": "The Discord channel ID"},
					"content":       map[string]any{"type": "string", "description": "The message content"},
					"credential_id": map[string]any{"type": "string", "description": "Optional bot credential ID in Vault"},
				},
				"required": []string{"channel_id", "content"},
			},
		},
	}
}

// CallTool executes a named tool with arguments.
func (t *Tools) CallTool(ctx context.Context, name string, args map[string]any) (*dispatch.EgressReceipt, error) {
	if args == nil {
		args = make(map[string]any)
	}

	switch name {
	case "slack_post_message":
		return t.callSlackPostMessage(ctx, args)
	case "jira_create_issue":
		return t.callJiraCreateIssue(ctx, args)
	case "twitter_post_tweet":
		return t.callTwitterPostTweet(ctx, args)
	case "discord_send_message":
		return t.callDiscordSendMessage(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool '%s'", name)
	}
}

func (t *Tools) callSlackPostMessage(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	channelID, _ := args["channel_id"].(string)
	text, _ := args["text"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["slack"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = "https://slack.com/api/chat.postMessage"
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel: "slack",
		Action:  "post_message",
		Method:  http.MethodPost,
		URL:     url,
		TokenID: tokenID,
		Body: map[string]any{
			"channel": channelID,
			"text":    text,
		},
	})
}

func (t *Tools) callJiraCreateIssue(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	projectKey, _ := args["project_key"].(string)
	summary, _ := args["summary"].(string)
	desc, _ := args["description"].(string)
	issueType, _ := args["issue_type"].(string)
	if issueType == "" {
		issueType = "Task"
	}

	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["jira"]
	}

	baseURL, _ := args["jira_base_url"].(string)
	if baseURL == "" {
		baseURL = "https://your-domain.atlassian.net"
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("%s/rest/api/3/issue", strings.TrimRight(baseURL, "/"))
	}

	body := map[string]any{
		"fields": map[string]any{
			"project":   map[string]any{"key": projectKey},
			"summary":   summary,
			"issuetype": map[string]any{"name": issueType},
		},
	}
	if desc != "" {
		body["fields"].(map[string]any)["description"] = map[string]any{
			"type":    "doc",
			"version": 1,
			"content": []map[string]any{
				{
					"type": "paragraph",
					"content": []map[string]any{
						{"type": "text", "text": desc},
					},
				},
			},
		}
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "jira",
		Action:       "create_issue",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callTwitterPostTweet(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	text, _ := args["text"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["twitter"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = "https://api.twitter.com/2/tweets"
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel: "twitter",
		Action:  "post_tweet",
		Method:  http.MethodPost,
		URL:     url,
		TokenID: tokenID,
		Body: map[string]any{
			"text": text,
		},
	})
}

func (t *Tools) callDiscordSendMessage(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	channelID, _ := args["channel_id"].(string)
	content, _ := args["content"].(string)
	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["discord"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "discord",
		Action:       "send_message",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body: map[string]any{
			"content": content,
		},
	})
}
