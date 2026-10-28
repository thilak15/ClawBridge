package inbound_test

import (
	"testing"

	"clawbridge/pkg/inbound"
)

func TestEventNormalizer_NormalizeSlackMessage(t *testing.T) {
	normalizer := inbound.NewEventNormalizer()

	webhook := &inbound.VerifiedWebhook{
		Channel:    "slack",
		DeliveryID: "deliv_123",
		Payload: map[string]any{
			"type": "event_callback",
			"event": map[string]any{
				"type": "message",
				"user": "U12345",
				"text": "Hello NemoClaw, check status",
			},
		},
		IsValid: true,
	}

	event, err := normalizer.Normalize(webhook)
	if err != nil {
		t.Fatalf("unexpected error normalizing: %v", err)
	}

	if event.Channel != "slack" || event.Topic != "slack:message" || event.EventType != "message" {
		t.Fatalf("unexpected topic or event_type: %+v", event)
	}
	if event.Author != "U12345" {
		t.Fatalf("expected author 'U12345', got '%s'", event.Author)
	}
	if event.Content != "Hello NemoClaw, check status" {
		t.Fatalf("expected content 'Hello NemoClaw, check status', got '%s'", event.Content)
	}
	if event.DeliveryID != "deliv_123" {
		t.Fatalf("expected delivery_id 'deliv_123', got '%s'", event.DeliveryID)
	}
}

func TestEventNormalizer_NormalizeJiraIssue(t *testing.T) {
	normalizer := inbound.NewEventNormalizer()

	webhook := &inbound.VerifiedWebhook{
		Channel:    "jira",
		DeliveryID: "jira_deliv_456",
		Payload: map[string]any{
			"webhookEvent": "jira:issue_created",
			"issue": map[string]any{
				"key": "ENG-404",
				"fields": map[string]any{
					"summary": "Fix auth gateway deadlock",
					"creator": map[string]any{
						"displayName": "Alex Developer",
					},
				},
			},
		},
		IsValid: true,
	}

	event, err := normalizer.Normalize(webhook)
	if err != nil {
		t.Fatalf("unexpected error normalizing jira: %v", err)
	}

	if event.Topic != "jira:issue_created" || event.Channel != "jira" {
		t.Fatalf("expected topic 'jira:issue_created', got '%s'", event.Topic)
	}
	if event.Author != "Alex Developer" {
		t.Fatalf("expected author 'Alex Developer', got '%s'", event.Author)
	}
	if event.Content != "Fix auth gateway deadlock" {
		t.Fatalf("expected content 'Fix auth gateway deadlock', got '%s'", event.Content)
	}
}

func TestEventNormalizer_NormalizeGeneric(t *testing.T) {
	normalizer := inbound.NewEventNormalizer()

	webhook := &inbound.VerifiedWebhook{
		Channel:    "custom",
		DeliveryID: "cust_789",
		Payload: map[string]any{
			"event": "alert",
			"user":  "monitor-bot",
			"text":  "High CPU load detected",
		},
		IsValid: true,
	}

	event, err := normalizer.Normalize(webhook)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Topic != "custom:alert" || event.Author != "monitor-bot" {
		t.Fatalf("unexpected generic event: %+v", event)
	}
}
