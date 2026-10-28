package vault_test

import (
	"path/filepath"
	"testing"

	"clawbridge/pkg/vault"
)

func TestDatabaseManager_InitializeAndWAL(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_clawbridge.db")

	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}
	defer dbMgr.Close()

	if err := dbMgr.Initialize(); err != nil {
		t.Fatalf("failed to initialize schema: %v", err)
	}

	// Verify WAL mode
	var journalMode string
	err = dbMgr.DB().QueryRow("PRAGMA journal_mode;").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode 'wal', got '%s'", journalMode)
	}

	// Verify busy timeout
	var busyTimeout int
	err = dbMgr.DB().QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("failed to query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout 5000, got %d", busyTimeout)
	}

	// Verify tables exist
	tables := []string{"credentials", "oauth_tokens", "webhook_events", "agents"}
	for _, table := range tables {
		var name string
		err := dbMgr.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?;", table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table '%s' to exist: %v", table, err)
		}
	}

	// Verify CheckpointWAL runs without error
	if err := dbMgr.CheckpointWAL(); err != nil {
		t.Fatalf("failed to checkpoint WAL: %v", err)
	}
}
