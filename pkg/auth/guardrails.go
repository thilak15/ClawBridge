package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"clawbridge/pkg/vault"
)

// ActionRisk defines the risk classification of an egress action.
type ActionRisk string

const (
	RiskLow  ActionRisk = "low"
	RiskHigh ActionRisk = "high"
)

// ClassifyActionRisk evaluates whether a tool represents a low-risk read/draft action
// or a high-risk external state change / communication that requires human approval.
func ClassifyActionRisk(toolName string) ActionRisk {
	lower := strings.ToLower(toolName)
	// Drafts and search/read queries are explicitly low risk
	if strings.Contains(lower, "draft") {
		return RiskLow
	}

	switch {
	case strings.Contains(lower, "post"),
		strings.Contains(lower, "send"),
		strings.Contains(lower, "publish"),
		strings.Contains(lower, "create"),
		strings.Contains(lower, "delete"),
		strings.Contains(lower, "update"):
		return RiskHigh
	default:
		return RiskLow
	}
}

// ApprovalRequest contains the context for a high-risk action awaiting human confirmation.
type ApprovalRequest struct {
	ID        string         `json:"id"`
	AgentName string         `json:"agent_name"`
	ToolName  string         `json:"tool_name"`
	Channel   string         `json:"channel"`
	Action    string         `json:"action"`
	Arguments map[string]any `json:"arguments"`
}

// GenerateApprovalID creates a unique identifier for an approval request.
func GenerateApprovalID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("appr_%s", hex.EncodeToString(b))
}

// ApprovalGate defines the interface for evaluating and authorizing high-risk actions.
type ApprovalGate interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error)
}

// AutoApprovalGate unconditionally approves all actions (used for tests or unrestricted daemon mode).
type AutoApprovalGate struct{}

func (g *AutoApprovalGate) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	return true, nil
}

// RejectAllApprovalGate unconditionally rejects all actions.
type RejectAllApprovalGate struct{}

func (g *RejectAllApprovalGate) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	return false, nil
}

// ConsoleApprovalGate prompts the human operator in the interactive console.
type ConsoleApprovalGate struct {
	in  io.Reader
	out io.Writer
	mu  sync.Mutex
}

// NewConsoleApprovalGate creates a new interactive console approval gate.
func NewConsoleApprovalGate(in io.Reader, out io.Writer) *ConsoleApprovalGate {
	return &ConsoleApprovalGate{in: in, out: out}
}

func (g *ConsoleApprovalGate) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	argsJSON, _ := json.MarshalIndent(req.Arguments, "", "  ")
	fmt.Fprintf(g.out, "\n\033[33m[ClawBridge Guardrail]\033[0m Agent \033[36m'%s'\033[0m requested high-risk action \033[32m'%s'\033[0m\n", req.AgentName, req.ToolName)
	fmt.Fprintf(g.out, "Payload:\n%s\n", string(argsJSON))
	fmt.Fprintf(g.out, "Approve execution? [y/N]: ")

	scanner := bufio.NewScanner(g.in)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer == "y" || answer == "yes" {
			fmt.Fprintf(g.out, "\033[32m[Guardrail]\033[0m Action '%s' approved by operator.\n", req.ToolName)
			return true, nil
		}
	}

	fmt.Fprintf(g.out, "\033[31m[Guardrail]\033[0m Action '%s' rejected by operator.\n", req.ToolName)
	return false, nil
}

// DatabaseApprovalGate queues high-risk actions in SQLite for asynchronous review.
type DatabaseApprovalGate struct {
	dbMgr *vault.DatabaseManager
}

// NewDatabaseApprovalGate creates a new database-backed approval gate.
func NewDatabaseApprovalGate(dbMgr *vault.DatabaseManager) *DatabaseApprovalGate {
	return &DatabaseApprovalGate{dbMgr: dbMgr}
}

func (g *DatabaseApprovalGate) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	if g.dbMgr == nil {
		return true, nil
	}

	argsJSON, _ := json.Marshal(req.Arguments)
	approvalID := req.ID
	if approvalID == "" {
		approvalID = GenerateApprovalID()
	}

	_, err := g.dbMgr.CreatePendingApproval(
		approvalID,
		req.AgentName,
		req.ToolName,
		req.Channel,
		req.Action,
		string(argsJSON),
	)
	if err != nil {
		return false, fmt.Errorf("failed to persist approval request: %w", err)
	}

	// In async queue mode, request is queued as pending, not immediately executed
	return false, fmt.Errorf("action held for human approval (approval_id: %s)", approvalID)
}
