package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
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
		{
			Name:        "telegram_send_message",
			Description: "Send a message to a Telegram chat or channel using an authenticated bot token.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"chat_id":       map[string]any{"type": "string", "description": "The Telegram chat ID or channel username (e.g. @channel)"},
					"text":          map[string]any{"type": "string", "description": "The text message content"},
					"parse_mode":    map[string]any{"type": "string", "description": "Optional parse mode (Markdown, HTML, or MarkdownV2)"},
					"credential_id": map[string]any{"type": "string", "description": "Optional bot credential ID in Vault"},
				},
				"required": []string{"chat_id", "text"},
			},
		},
		{
			Name:        "telegram_send_photo",
			Description: "Send a photo to a Telegram chat or channel with an optional caption.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"chat_id":       map[string]any{"type": "string", "description": "The Telegram chat ID"},
					"photo_url":     map[string]any{"type": "string", "description": "Direct HTTP URL of the photo to send"},
					"caption":       map[string]any{"type": "string", "description": "Optional photo caption"},
					"credential_id": map[string]any{"type": "string", "description": "Optional bot credential ID in Vault"},
				},
				"required": []string{"chat_id", "photo_url"},
			},
		},
		{
			Name:        "whatsapp_send_message",
			Description: "Send a text message to a WhatsApp user using the WhatsApp Cloud API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":              map[string]any{"type": "string", "description": "Recipient phone number with country code (e.g. +14155552671)"},
					"text":            map[string]any{"type": "string", "description": "The text message content"},
					"phone_number_id": map[string]any{"type": "string", "description": "Optional sender Phone Number ID"},
					"credential_id":   map[string]any{"type": "string", "description": "Optional WhatsApp credential ID in Vault"},
				},
				"required": []string{"to", "text"},
			},
		},
		{
			Name:        "whatsapp_send_template",
			Description: "Send a pre-approved template message to a WhatsApp user using the WhatsApp Cloud API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":              map[string]any{"type": "string", "description": "Recipient phone number with country code"},
					"template_name":   map[string]any{"type": "string", "description": "Name of the approved template"},
					"language_code":   map[string]any{"type": "string", "description": "Language code (default: en_US)"},
					"phone_number_id": map[string]any{"type": "string", "description": "Optional sender Phone Number ID"},
					"credential_id":   map[string]any{"type": "string", "description": "Optional WhatsApp credential ID in Vault"},
				},
				"required": []string{"to", "template_name"},
			},
		},
		{
			Name:        "instagram_post_publish",
			Description: "Create and publish a photo post on an Instagram Professional account via Meta Graph API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image_url":     map[string]any{"type": "string", "description": "Public URL of the image container to publish"},
					"caption":       map[string]any{"type": "string", "description": "Post caption with hashtags"},
					"ig_user_id":    map[string]any{"type": "string", "description": "Instagram User ID (default: me)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Meta credential ID in Vault"},
				},
				"required": []string{"image_url"},
			},
		},
		{
			Name:        "instagram_read_comments",
			Description: "Retrieve recent comments on an Instagram media post via Meta Graph API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"media_id":      map[string]any{"type": "string", "description": "The Instagram media ID"},
					"token_id":      map[string]any{"type": "string", "description": "Optional OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Meta credential ID in Vault"},
				},
				"required": []string{"media_id"},
			},
		},
		{
			Name:        "instagram_reply_comment",
			Description: "Post a reply to a user comment on Instagram via Meta Graph API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"comment_id":    map[string]any{"type": "string", "description": "The Instagram comment ID to reply to"},
					"message":       map[string]any{"type": "string", "description": "The reply message text"},
					"token_id":      map[string]any{"type": "string", "description": "Optional OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Meta credential ID in Vault"},
				},
				"required": []string{"comment_id", "message"},
			},
		},
		{
			Name:        "facebook_post_feed",
			Description: "Publish a post or link to a Facebook Page feed via Meta Graph API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message":       map[string]any{"type": "string", "description": "The post body text"},
					"link":          map[string]any{"type": "string", "description": "Optional link URL to attach"},
					"page_id":       map[string]any{"type": "string", "description": "Facebook Page ID (default: me)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Meta credential ID in Vault"},
				},
				"required": []string{"message"},
			},
		},
		{
			Name:        "facebook_read_page_feed",
			Description: "Read recent status posts from a Facebook Page feed via Meta Graph API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id":       map[string]any{"type": "string", "description": "Facebook Page ID (default: me)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Meta credential ID in Vault"},
				},
			},
		},
		{
			Name:        "calendar_list_events",
			Description: "List calendar events within a specified time range via Google Calendar API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"calendar_id":   map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
					"time_min":      map[string]any{"type": "string", "description": "Lower bound for event start time (RFC3339)"},
					"time_max":      map[string]any{"type": "string", "description": "Upper bound for event start time (RFC3339)"},
					"max_results":   map[string]any{"type": "integer", "description": "Maximum events to return (default 25)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
			},
		},
		{
			Name:        "calendar_find_free_slots",
			Description: "Query busy/free intervals across calendars to discover open meeting slots via Google Calendar FreeBusy API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"calendar_id":   map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
					"time_min":      map[string]any{"type": "string", "description": "Start of query window (RFC3339)"},
					"time_max":      map[string]any{"type": "string", "description": "End of query window (RFC3339)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"time_min", "time_max"},
			},
		},
		{
			Name:        "calendar_create_event",
			Description: "Create a calendar event with attendees and schedule via Google Calendar API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary":       map[string]any{"type": "string", "description": "Event title/summary"},
					"description":   map[string]any{"type": "string", "description": "Event detailed description"},
					"start_time":    map[string]any{"type": "string", "description": "Start time in RFC3339 format"},
					"end_time":      map[string]any{"type": "string", "description": "End time in RFC3339 format"},
					"attendees":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "List of attendee email addresses"},
					"calendar_id":   map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"summary", "start_time", "end_time"},
			},
		},
		{
			Name:        "gmail_search_messages",
			Description: "Search user emails using standard Gmail search queries (e.g. 'is:unread', 'from:sender').",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":         map[string]any{"type": "string", "description": "Gmail search query string"},
					"max_results":   map[string]any{"type": "integer", "description": "Maximum messages to return (default 10)"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "gmail_read_thread",
			Description: "Read the full contents and message headers of an email thread via Gmail API.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"thread_id":     map[string]any{"type": "string", "description": "The Gmail thread ID"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"thread_id"},
			},
		},
		{
			Name:        "gmail_create_draft",
			Description: "Create a draft email in Gmail without sending it.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":            map[string]any{"type": "string", "description": "Recipient email address"},
					"subject":       map[string]any{"type": "string", "description": "Email subject line"},
					"body":          map[string]any{"type": "string", "description": "Plain text body of the email"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"to", "subject", "body"},
			},
		},
		{
			Name:        "gmail_send_message",
			Description: "Send an email message via Gmail API (requires Human-in-the-Loop guardrail approval).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":            map[string]any{"type": "string", "description": "Recipient email address"},
					"subject":       map[string]any{"type": "string", "description": "Email subject line"},
					"body":          map[string]any{"type": "string", "description": "Plain text body of the email"},
					"token_id":      map[string]any{"type": "string", "description": "Optional Google OAuth token ID in Vault"},
					"credential_id": map[string]any{"type": "string", "description": "Optional Google credential ID in Vault"},
				},
				"required": []string{"to", "subject", "body"},
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
	case "telegram_send_message":
		return t.callTelegramSendMessage(ctx, args)
	case "telegram_send_photo":
		return t.callTelegramSendPhoto(ctx, args)
	case "whatsapp_send_message":
		return t.callWhatsAppSendMessage(ctx, args)
	case "whatsapp_send_template":
		return t.callWhatsAppSendTemplate(ctx, args)
	case "instagram_post_publish":
		return t.callInstagramPostPublish(ctx, args)
	case "instagram_read_comments":
		return t.callInstagramReadComments(ctx, args)
	case "instagram_reply_comment":
		return t.callInstagramReplyComment(ctx, args)
	case "facebook_post_feed":
		return t.callFacebookPostFeed(ctx, args)
	case "facebook_read_page_feed":
		return t.callFacebookReadPageFeed(ctx, args)
	case "calendar_list_events":
		return t.callCalendarListEvents(ctx, args)
	case "calendar_find_free_slots":
		return t.callCalendarFindFreeSlots(ctx, args)
	case "calendar_create_event":
		return t.callCalendarCreateEvent(ctx, args)
	case "gmail_search_messages":
		return t.callGmailSearchMessages(ctx, args)
	case "gmail_read_thread":
		return t.callGmailReadThread(ctx, args)
	case "gmail_create_draft":
		return t.callGmailCreateDraft(ctx, args)
	case "gmail_send_message":
		return t.callGmailSendMessage(ctx, args)
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

func (t *Tools) callTelegramSendMessage(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	chatID, _ := args["chat_id"].(string)
	text, _ := args["text"].(string)
	parseMode, _ := args["parse_mode"].(string)

	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["telegram"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = "https://api.telegram.org/sendMessage"
	}

	body := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		body["parse_mode"] = parseMode
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "telegram",
		Action:       "send_message",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callTelegramSendPhoto(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	chatID, _ := args["chat_id"].(string)
	photoURL, _ := args["photo_url"].(string)
	caption, _ := args["caption"].(string)

	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["telegram"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = "https://api.telegram.org/sendPhoto"
	}

	body := map[string]any{
		"chat_id": chatID,
		"photo":   photoURL,
	}
	if caption != "" {
		body["caption"] = caption
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "telegram",
		Action:       "send_photo",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callWhatsAppSendMessage(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	to, _ := args["to"].(string)
	text, _ := args["text"].(string)
	phoneID, _ := args["phone_number_id"].(string)
	if phoneID == "" {
		phoneID = "default"
	}

	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["whatsapp"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/messages", phoneID)
	}

	body := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text": map[string]any{
			"body": text,
		},
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "whatsapp",
		Action:       "send_message",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callWhatsAppSendTemplate(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	to, _ := args["to"].(string)
	templateName, _ := args["template_name"].(string)
	langCode, _ := args["language_code"].(string)
	if langCode == "" {
		langCode = "en_US"
	}
	phoneID, _ := args["phone_number_id"].(string)
	if phoneID == "" {
		phoneID = "default"
	}

	credID, _ := args["credential_id"].(string)
	if credID == "" {
		credID = t.defaultCreds["whatsapp"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/messages", phoneID)
	}

	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]any{
			"name": templateName,
			"language": map[string]any{
				"code": langCode,
			},
		},
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "whatsapp",
		Action:       "send_template",
		Method:       http.MethodPost,
		URL:          url,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callInstagramPostPublish(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	imageURL, _ := args["image_url"].(string)
	caption, _ := args["caption"].(string)
	igUserID, _ := args["ig_user_id"].(string)
	if igUserID == "" {
		igUserID = "me"
	}

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["instagram"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["instagram"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/media", igUserID)
	}

	body := map[string]any{
		"image_url": imageURL,
		"caption":   caption,
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "instagram",
		Action:       "post_publish",
		Method:       http.MethodPost,
		URL:          url,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callInstagramReadComments(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	mediaID, _ := args["media_id"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["instagram"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["instagram"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/comments", mediaID)
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "instagram",
		Action:       "read_comments",
		Method:       http.MethodGet,
		URL:          url,
		TokenID:      tokenID,
		CredentialID: credID,
	})
}

func (t *Tools) callInstagramReplyComment(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	commentID, _ := args["comment_id"].(string)
	message, _ := args["message"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["instagram"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["instagram"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/replies", commentID)
	}

	body := map[string]any{
		"message": message,
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "instagram",
		Action:       "reply_comment",
		Method:       http.MethodPost,
		URL:          url,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callFacebookPostFeed(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	message, _ := args["message"].(string)
	link, _ := args["link"].(string)
	pageID, _ := args["page_id"].(string)
	if pageID == "" {
		pageID = "me"
	}

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["facebook"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["facebook"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/feed", pageID)
	}

	body := map[string]any{
		"message": message,
	}
	if link != "" {
		body["link"] = link
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "facebook",
		Action:       "post_feed",
		Method:       http.MethodPost,
		URL:          url,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callFacebookReadPageFeed(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	pageID, _ := args["page_id"].(string)
	if pageID == "" {
		pageID = "me"
	}

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["facebook"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["facebook"]
	}

	url, _ := args["url"].(string)
	if url == "" {
		url = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/feed", pageID)
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "facebook",
		Action:       "read_page_feed",
		Method:       http.MethodGet,
		URL:          url,
		TokenID:      tokenID,
		CredentialID: credID,
	})
}

func (t *Tools) callCalendarListEvents(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	calID, _ := args["calendar_id"].(string)
	if calID == "" {
		calID = "primary"
	}
	timeMin, _ := args["time_min"].(string)
	timeMax, _ := args["time_max"].(string)

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		params := url.Values{}
		if timeMin != "" {
			params.Set("timeMin", timeMin)
		}
		if timeMax != "" {
			params.Set("timeMax", timeMax)
		}
		params.Set("singleEvents", "true")
		params.Set("orderBy", "startTime")
		urlStr = fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events?%s", url.PathEscape(calID), params.Encode())
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "calendar",
		Action:       "list_events",
		Method:       http.MethodGet,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
	})
}

func (t *Tools) callCalendarFindFreeSlots(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	calID, _ := args["calendar_id"].(string)
	if calID == "" {
		calID = "primary"
	}
	timeMin, _ := args["time_min"].(string)
	timeMax, _ := args["time_max"].(string)

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		urlStr = "https://www.googleapis.com/calendar/v3/freeBusy"
	}

	body := map[string]any{
		"timeMin": timeMin,
		"timeMax": timeMax,
		"items": []map[string]any{
			{"id": calID},
		},
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "calendar",
		Action:       "find_free_slots",
		Method:       http.MethodPost,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callCalendarCreateEvent(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	calID, _ := args["calendar_id"].(string)
	if calID == "" {
		calID = "primary"
	}
	summary, _ := args["summary"].(string)
	description, _ := args["description"].(string)
	startTime, _ := args["start_time"].(string)
	endTime, _ := args["end_time"].(string)

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		urlStr = fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events", url.PathEscape(calID))
	}

	body := map[string]any{
		"summary":     summary,
		"description": description,
		"start":       map[string]any{"dateTime": startTime},
		"end":         map[string]any{"dateTime": endTime},
	}

	if atts, ok := args["attendees"].([]any); ok && len(atts) > 0 {
		var attendeesList []map[string]string
		for _, a := range atts {
			if email, ok := a.(string); ok && email != "" {
				attendeesList = append(attendeesList, map[string]string{"email": email})
			}
		}
		if len(attendeesList) > 0 {
			body["attendees"] = attendeesList
		}
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "calendar",
		Action:       "create_event",
		Method:       http.MethodPost,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callGmailSearchMessages(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	q, _ := args["query"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		params := url.Values{}
		params.Set("q", q)
		urlStr = fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages?%s", params.Encode())
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "gmail",
		Action:       "search_messages",
		Method:       http.MethodGet,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
	})
}

func (t *Tools) callGmailReadThread(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	threadID, _ := args["thread_id"].(string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		urlStr = fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/threads/%s", url.PathEscape(threadID))
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "gmail",
		Action:       "read_thread",
		Method:       http.MethodGet,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
	})
}

func (t *Tools) callGmailCreateDraft(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	msgBody, _ := args["body"].(string)

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	rawEmail := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", to, subject, msgBody)
	encodedRaw := base64.URLEncoding.EncodeToString([]byte(rawEmail))

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		urlStr = "https://gmail.googleapis.com/gmail/v1/users/me/drafts"
	}

	body := map[string]any{
		"message": map[string]any{
			"raw": encodedRaw,
		},
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "gmail",
		Action:       "create_draft",
		Method:       http.MethodPost,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}

func (t *Tools) callGmailSendMessage(ctx context.Context, args map[string]any) (*dispatch.EgressReceipt, error) {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	msgBody, _ := args["body"].(string)

	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = t.defaultTokens["google"]
	}
	credID, _ := args["credential_id"].(string)
	if credID == "" && tokenID == "" {
		credID = t.defaultCreds["google"]
	}

	rawEmail := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", to, subject, msgBody)
	encodedRaw := base64.URLEncoding.EncodeToString([]byte(rawEmail))

	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		urlStr = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
	}

	body := map[string]any{
		"raw": encodedRaw,
	}

	return t.dispatcher.Dispatch(ctx, dispatch.DispatchRequest{
		Channel:      "gmail",
		Action:       "send_message",
		Method:       http.MethodPost,
		URL:          urlStr,
		TokenID:      tokenID,
		CredentialID: credID,
		Body:         body,
	})
}
