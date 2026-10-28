package inbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// InboundEvent is the canonical normalized representation of an incoming platform webhook.
type InboundEvent struct {
	EventID    string         `json:"event_id"`
	Channel    string         `json:"channel"`
	EventType  string         `json:"event_type"`
	Topic      string         `json:"topic"`
	DeliveryID string         `json:"delivery_id"`
	Timestamp  string         `json:"timestamp"`
	Author     string         `json:"author,omitempty"`
	Content    string         `json:"content,omitempty"`
	Data       map[string]any `json:"data"`
}

// EventNormalizer transforms diverse verified platform webhooks into structured InboundEvents.
type EventNormalizer struct{}

// NewEventNormalizer creates a new EventNormalizer instance.
func NewEventNormalizer() *EventNormalizer {
	return &EventNormalizer{}
}

func generateEventID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt_%s", hex.EncodeToString(b))
}

// Normalize maps a VerifiedWebhook to a canonical InboundEvent.
func (n *EventNormalizer) Normalize(webhook *VerifiedWebhook) (*InboundEvent, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	eventID := generateEventID()
	channel := strings.ToLower(webhook.Channel)
	payload := webhook.Payload
	if payload == nil {
		payload = make(map[string]any)
	}

	switch channel {
	case "slack":
		return n.normalizeSlack(eventID, webhook.DeliveryID, now, payload), nil
	case "jira":
		return n.normalizeJira(eventID, webhook.DeliveryID, now, payload), nil
	default:
		return n.normalizeGeneric(channel, eventID, webhook.DeliveryID, now, payload), nil
	}
}

func (n *EventNormalizer) normalizeSlack(eventID, deliveryID, now string, payload map[string]any) *InboundEvent {
	pType, _ := payload["type"].(string)

	if pType == "url_verification" {
		return &InboundEvent{
			EventID:    eventID,
			Channel:    "slack",
			EventType:  "url_verification",
			Topic:      "slack:system",
			DeliveryID: deliveryID,
			Timestamp:  now,
			Data:       payload,
		}
	}

	if pType == "event_callback" {
		if ev, ok := payload["event"].(map[string]any); ok {
			subType, _ := ev["type"].(string)
			if subType == "" {
				subType = "event"
			}
			author, _ := ev["user"].(string)
			text, _ := ev["text"].(string)

			return &InboundEvent{
				EventID:    eventID,
				Channel:    "slack",
				EventType:  subType,
				Topic:      fmt.Sprintf("slack:%s", subType),
				DeliveryID: deliveryID,
				Timestamp:  now,
				Author:     author,
				Content:    text,
				Data:       payload,
			}
		}
	}

	return &InboundEvent{
		EventID:    eventID,
		Channel:    "slack",
		EventType:  pType,
		Topic:      fmt.Sprintf("slack:%s", pType),
		DeliveryID: deliveryID,
		Timestamp:  now,
		Data:       payload,
	}
}

func (n *EventNormalizer) normalizeJira(eventID, deliveryID, now string, payload map[string]any) *InboundEvent {
	rawEvent, _ := payload["webhookEvent"].(string)
	if rawEvent == "" {
		rawEvent = "jira:issue_updated"
	}

	eventType := rawEvent
	if parts := strings.Split(rawEvent, ":"); len(parts) > 1 {
		eventType = parts[len(parts)-1]
	}

	var author, content string
	if issue, ok := payload["issue"].(map[string]any); ok {
		if fields, ok := issue["fields"].(map[string]any); ok {
			if summary, ok := fields["summary"].(string); ok {
				content = summary
			}
			if creator, ok := fields["creator"].(map[string]any); ok {
				if dName, ok := creator["displayName"].(string); ok && dName != "" {
					author = dName
				} else if name, ok := creator["name"].(string); ok {
					author = name
				}
			}
		}
	}

	return &InboundEvent{
		EventID:    eventID,
		Channel:    "jira",
		EventType:  eventType,
		Topic:      rawEvent,
		DeliveryID: deliveryID,
		Timestamp:  now,
		Author:     author,
		Content:    content,
		Data:       payload,
	}
}

func (n *EventNormalizer) normalizeGeneric(channel, eventID, deliveryID, now string, payload map[string]any) *InboundEvent {
	var author, content string

	for _, key := range []string{"user", "author", "sender", "username"} {
		if val, ok := payload[key].(string); ok && val != "" {
			author = val
			break
		}
	}

	for _, key := range []string{"text", "content", "message", "body", "title"} {
		if val, ok := payload[key].(string); ok && val != "" {
			content = val
			break
		}
	}

	eventType := "event"
	if ev, ok := payload["event"].(string); ok && ev != "" {
		eventType = ev
	} else if action, ok := payload["action"].(string); ok && action != "" {
		eventType = action
	}

	return &InboundEvent{
		EventID:    eventID,
		Channel:    channel,
		EventType:  eventType,
		Topic:      fmt.Sprintf("%s:%s", channel, eventType),
		DeliveryID: deliveryID,
		Timestamp:  now,
		Author:     author,
		Content:    content,
		Data:       payload,
	}
}
