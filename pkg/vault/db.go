package vault

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DatabaseManager handles SQLite database connection, schema migration, and WAL maintenance.
type DatabaseManager struct {
	db   *sql.DB
	path string
}

// NewDatabaseManager opens or creates an SQLite database at the specified path.
func NewDatabaseManager(path string) (*DatabaseManager, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory for database: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Restrict to single writer to avoid SQLite locking conflicts while allowing concurrent readers
	db.SetMaxOpenConns(1)

	return &DatabaseManager{
		db:   db,
		path: path,
	}, nil
}

// Initialize creates all required tables and indexes if they do not exist.
func (m *DatabaseManager) Initialize() error {
	schema := `
	PRAGMA journal_mode=WAL;
	PRAGMA busy_timeout=5000;
	PRAGMA synchronous=NORMAL;

	CREATE TABLE IF NOT EXISTS credentials (
		id TEXT PRIMARY KEY,
		channel TEXT NOT NULL,
		auth_type TEXT NOT NULL,
		encrypted_blob TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_credentials_channel ON credentials(channel);

	CREATE TABLE IF NOT EXISTS oauth_tokens (
		id TEXT PRIMARY KEY,
		channel TEXT NOT NULL,
		client_id TEXT NOT NULL,
		encrypted_blob TEXT NOT NULL,
		expires_at REAL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_oauth_tokens_channel ON oauth_tokens(channel);

	CREATE TABLE IF NOT EXISTS webhook_events (
		id TEXT PRIMARY KEY,
		channel TEXT NOT NULL,
		delivery_id TEXT NOT NULL UNIQUE,
		payload_json TEXT NOT NULL,
		status TEXT NOT NULL,
		received_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_webhook_events_channel ON webhook_events(channel);
	CREATE INDEX IF NOT EXISTS idx_webhook_events_delivery ON webhook_events(delivery_id);

	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		token_hash TEXT NOT NULL UNIQUE,
		allowed_rules_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);

	CREATE TABLE IF NOT EXISTS agent_memories (
		agent_name TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		turn_count INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_agent_memories_agent ON agent_memories(agent_name);
	`

	if _, err := m.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

// CheckpointWAL truncates the Write-Ahead Log to keep disk usage under control.
func (m *DatabaseManager) CheckpointWAL() error {
	if _, err := m.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		return fmt.Errorf("failed to checkpoint WAL: %w", err)
	}
	return nil
}

// SaveAgentMemory stores or updates the rolling summary for an agent in SQLite WAL.
func (m *DatabaseManager) SaveAgentMemory(agentName, summary string, turnCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `
	INSERT INTO agent_memories (agent_name, summary, turn_count, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(agent_name) DO UPDATE SET
		summary = excluded.summary,
		turn_count = excluded.turn_count,
		updated_at = excluded.updated_at;
	`
	_, err := m.db.Exec(query, agentName, summary, turnCount, now)
	return err
}

// GetAgentMemory retrieves the rolling summary and turn count for an agent.
func (m *DatabaseManager) GetAgentMemory(agentName string) (string, int, error) {
	query := `SELECT summary, turn_count FROM agent_memories WHERE agent_name = ?;`
	var summary string
	var turnCount int
	err := m.db.QueryRow(query, agentName).Scan(&summary, &turnCount)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return summary, turnCount, nil
}

// DB returns the underlying sql.DB instance.
func (m *DatabaseManager) DB() *sql.DB {
	return m.db
}

// Close closes the underlying database connection.
func (m *DatabaseManager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}
