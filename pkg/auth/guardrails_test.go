package auth_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"clawbridge/pkg/auth"
	"clawbridge/pkg/vault"
)

func TestClassifyActionRisk(t *testing.T) {
	highRiskTools := []string{
		"twitter_post_tweet",
		"instagram_post_publish",
		"facebook_post_feed",
		"gmail_send_message",
		"whatsapp_send_message",
		"slack_post_message",
		"discord_send_message",
		"jira_create_issue",
		"calendar_create_event",
	}

	for _, tool := range highRiskTools {
		risk := auth.ClassifyActionRisk(tool)
		if risk != auth.RiskHigh {
			t.Errorf("expected '%s' to be high risk, got %v", tool, risk)
		}
	}

	lowRiskTools := []string{
		"calendar_list_events",
		"calendar_find_free_slots",
		"gmail_search_messages",
		"gmail_read_thread",
		"gmail_create_draft",
		"instagram_read_comments",
		"slack_list_channels",
	}

	for _, tool := range lowRiskTools {
		risk := auth.ClassifyActionRisk(tool)
		if risk != auth.RiskLow {
			t.Errorf("expected '%s' to be low risk, got %v", tool, risk)
		}
	}
}

func TestConsoleApprovalGate(t *testing.T) {
	req := auth.ApprovalRequest{
		ID:        "appr_123",
		AgentName: "nemoclaw",
		ToolName:  "twitter_post_tweet",
		Channel:   "twitter",
		Action:    "post_tweet",
		Arguments: map[string]any{"text": "Hello world!"},
	}

	// Test approval when operator inputs 'y'
	inApproved := strings.NewReader("y\n")
	outBufApproved := &bytes.Buffer{}
	gateApproved := auth.NewConsoleApprovalGate(inApproved, outBufApproved)

	ok, err := gateApproved.RequestApproval(context.Background(), req)
	if err != nil || !ok {
		t.Fatalf("expected approval, got ok=%v, err=%v", ok, err)
	}
	if !strings.Contains(outBufApproved.String(), "approved by operator") {
		t.Errorf("expected approval confirmation in output, got: %s", outBufApproved.String())
	}

	// Test rejection when operator inputs 'n'
	inRejected := strings.NewReader("n\n")
	outBufRejected := &bytes.Buffer{}
	gateRejected := auth.NewConsoleApprovalGate(inRejected, outBufRejected)

	ok, err = gateRejected.RequestApproval(context.Background(), req)
	if ok {
		t.Fatalf("expected rejection, got ok=%v", ok)
	}
	if !strings.Contains(outBufRejected.String(), "rejected by operator") {
		t.Errorf("expected rejection confirmation in output, got: %s", outBufRejected.String())
	}
}

func TestDatabaseApprovalGate_QueueAndResolve(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "approvals.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}
	defer dbMgr.Close()
	_ = dbMgr.Initialize()

	gate := auth.NewDatabaseApprovalGate(dbMgr)
	req := auth.ApprovalRequest{
		ID:        "appr_test_001",
		AgentName: "nemoclaw",
		ToolName:  "gmail_send_message",
		Channel:   "gmail",
		Action:    "send_message",
		Arguments: map[string]any{"to": "user@example.com", "subject": "Hello"},
	}

	ok, err := gate.RequestApproval(context.Background(), req)
	if ok || err == nil {
		t.Fatalf("expected queue rejection hold, got ok=%v, err=%v", ok, err)
	}

	// Check that the request was stored in pending_approvals
	list, err := dbMgr.ListPendingApprovals("pending")
	if err != nil {
		t.Fatalf("failed to list pending approvals: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(list))
	}
	if list[0].ID != "appr_test_001" || list[0].Status != "pending" {
		t.Errorf("unexpected record: %+v", list[0])
	}

	// Resolve approval
	if err := dbMgr.ResolveApproval("appr_test_001", "approved"); err != nil {
		t.Fatalf("failed to resolve approval: %v", err)
	}

	rec, err := dbMgr.GetPendingApproval("appr_test_001")
	if err != nil {
		t.Fatalf("failed to get approval: %v", err)
	}
	if rec.Status != "approved" {
		t.Errorf("expected status 'approved', got '%s'", rec.Status)
	}
}
