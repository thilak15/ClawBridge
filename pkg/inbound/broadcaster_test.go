package inbound_test

import (
	"strings"
	"testing"
	"time"

	"clawbridge/pkg/inbound"
)

func TestEventBroadcaster_PublishSubscribeTopicFilter(t *testing.T) {
	b := inbound.NewEventBroadcaster()

	// NemoClaw subscribes only to Slack topics
	slackSub := b.Subscribe([]string{"slack:*"})
	// OpenClaw subscribes to all topics
	allSub := b.Subscribe([]string{"*"})

	slackEv := &inbound.InboundEvent{
		EventID:   "evt_01",
		Channel:   "slack",
		EventType: "message",
		Topic:     "slack:message",
		Content:   "Alert for NemoClaw",
	}

	jiraEv := &inbound.InboundEvent{
		EventID:   "evt_02",
		Channel:   "jira",
		EventType: "issue_created",
		Topic:     "jira:issue_created",
		Content:   "New task for OpenClaw",
	}

	delivered1 := b.Publish(slackEv)
	if delivered1 != 2 {
		t.Fatalf("expected slackEv delivered to 2 subscribers, delivered to %d", delivered1)
	}

	delivered2 := b.Publish(jiraEv)
	if delivered2 != 1 {
		t.Fatalf("expected jiraEv delivered to 1 subscriber (allSub), delivered to %d", delivered2)
	}

	// Verify slackSub received slackEv
	select {
	case ev := <-slackSub:
		if ev.EventID != "evt_01" {
			t.Fatalf("expected evt_01, got %s", ev.EventID)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("timeout waiting for slackSub event")
	}

	// Verify slackSub does NOT receive jiraEv
	select {
	case ev := <-slackSub:
		t.Fatalf("unexpected event received by slackSub: %+v", ev)
	case <-time.After(20 * time.Millisecond):
		// Expected: nothing received
	}

	// Clean up
	b.Unsubscribe(slackSub)
	b.Unsubscribe(allSub)
}

func TestEventBroadcaster_FormatSSEMessage(t *testing.T) {
	b := inbound.NewEventBroadcaster()

	ev := &inbound.InboundEvent{
		EventID:   "evt_999",
		Channel:   "slack",
		EventType: "message",
		Topic:     "slack:message",
		Content:   "Hello World",
	}

	sse := b.FormatSSEMessage(ev)

	if !strings.HasPrefix(sse, "id: evt_999\n") {
		t.Fatalf("expected 'id: evt_999', got:\n%s", sse)
	}
	if !strings.Contains(sse, "event: slack:message\n") {
		t.Fatalf("expected 'event: slack:message', got:\n%s", sse)
	}
	if !strings.Contains(sse, "data: ") {
		t.Fatalf("expected 'data: ...', got:\n%s", sse)
	}
	if !strings.HasSuffix(sse, "\n\n") {
		t.Fatalf("expected double newline suffix, got:\n%s", sse)
	}
}
