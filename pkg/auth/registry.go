package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"clawbridge/pkg/vault"
)

// AgentRecord represents a registered agent in SQLite.
type AgentRecord struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TokenHash    string    `json:"token_hash"`
	AllowedRules []string  `json:"allowed_rules"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AgentRegistry manages agent registration, authentication, and permission policies in SQLite.
type AgentRegistry struct {
	dbMgr *vault.DatabaseManager
}

// NewAgentRegistry creates a new AgentRegistry instance.
func NewAgentRegistry(dbMgr *vault.DatabaseManager) *AgentRegistry {
	return &AgentRegistry{dbMgr: dbMgr}
}

// DatabaseManager returns the underlying vault DatabaseManager.
func (r *AgentRegistry) DatabaseManager() *vault.DatabaseManager {
	return r.dbMgr
}

// RegisterAgent registers a new agent and returns the record and plaintext bearer token.
func (r *AgentRegistry) RegisterAgent(name string, rules []string) (*AgentRecord, string, error) {
	if len(rules) == 0 {
		rules = []string{"*"}
	}

	rawToken, tokenHash, err := GenerateAgentToken()
	if err != nil {
		return nil, "", err
	}

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	agentID := fmt.Sprintf("agt_%s", hex.EncodeToString(b))

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal allowed rules: %w", err)
	}

	query := `
	INSERT INTO agents (id, name, token_hash, allowed_rules_json, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?);
	`
	_, err = r.dbMgr.DB().Exec(query, agentID, name, tokenHash, string(rulesJSON), nowStr, nowStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to insert agent into database: %w", err)
	}

	record := &AgentRecord{
		ID:           agentID,
		Name:         name,
		TokenHash:    tokenHash,
		AllowedRules: rules,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	return record, rawToken, nil
}

// GetAgentByName finds an agent record by name.
func (r *AgentRegistry) GetAgentByName(name string) (*AgentRecord, error) {
	query := `
	SELECT id, name, token_hash, allowed_rules_json, created_at, updated_at
	FROM agents WHERE name = ?;
	`
	var rec AgentRecord
	var rulesJSON, createdAtStr, updatedAtStr string

	err := r.dbMgr.DB().QueryRow(query, name).Scan(
		&rec.ID,
		&rec.Name,
		&rec.TokenHash,
		&rulesJSON,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("agent '%s' not found", name)
		}
		return nil, fmt.Errorf("failed to query agent: %w", err)
	}

	_ = json.Unmarshal([]byte(rulesJSON), &rec.AllowedRules)
	rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &rec, nil
}

// GetAgentByToken verifies a bearer token against the database and returns the matching agent.
func (r *AgentRegistry) GetAgentByToken(rawToken string) (*AgentRecord, error) {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	query := `
	SELECT id, name, token_hash, allowed_rules_json, created_at, updated_at
	FROM agents WHERE token_hash = ?;
	`
	var rec AgentRecord
	var rulesJSON, createdAtStr, updatedAtStr string

	err := r.dbMgr.DB().QueryRow(query, tokenHash).Scan(
		&rec.ID,
		&rec.Name,
		&rec.TokenHash,
		&rulesJSON,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid agent token")
		}
		return nil, fmt.Errorf("failed to query agent by token: %w", err)
	}

	_ = json.Unmarshal([]byte(rulesJSON), &rec.AllowedRules)
	rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &rec, nil
}

// UpdateAgentRules updates the allowed rules for an existing agent.
func (r *AgentRegistry) UpdateAgentRules(name string, rules []string) error {
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("failed to marshal rules: %w", err)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	query := `UPDATE agents SET allowed_rules_json = ?, updated_at = ? WHERE name = ?;`

	res, err := r.dbMgr.DB().Exec(query, string(rulesJSON), nowStr, name)
	if err != nil {
		return fmt.Errorf("failed to update agent rules: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent '%s' not found to update rules", name)
	}

	return nil
}

// ListAgents returns all registered agents in the database.
func (r *AgentRegistry) ListAgents() ([]*AgentRecord, error) {
	query := `SELECT id, name, token_hash, allowed_rules_json, created_at, updated_at FROM agents ORDER BY name ASC;`
	rows, err := r.dbMgr.DB().Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	var agents []*AgentRecord
	for rows.Next() {
		var rec AgentRecord
		var rulesJSON, createdAtStr, updatedAtStr string
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.TokenHash, &rulesJSON, &createdAtStr, &updatedAtStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(rulesJSON), &rec.AllowedRules)
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
		agents = append(agents, &rec)
	}

	return agents, nil
}

