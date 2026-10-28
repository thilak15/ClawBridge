import sqlite3
import pytest
from pathlib import Path
from clawbridge.vault.db import DatabaseManager


def test_db_manager_initializes_wal_and_busy_timeout(tmp_path: Path):
    db_file = tmp_path / "vault.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    
    with db_mgr.connection() as conn:
        journal_mode = conn.execute("PRAGMA journal_mode").fetchone()[0]
        assert journal_mode.upper() == "WAL"
        
        busy_timeout = conn.execute("PRAGMA busy_timeout").fetchone()[0]
        assert busy_timeout == 5000
        
        foreign_keys = conn.execute("PRAGMA foreign_keys").fetchone()[0]
        assert foreign_keys == 1


def test_schema_migrations_applied_idempotently(tmp_path: Path):
    db_file = tmp_path / "vault.db"
    db_mgr = DatabaseManager(db_file)
    
    # Run once
    db_mgr.initialize()
    with db_mgr.connection() as conn:
        tables = {row[0] for row in conn.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()}
        assert "schema_migrations" in tables
        assert "credentials" in tables
        assert "oauth_tokens" in tables
        assert "audit_log" in tables
        
        # Verify migration record
        version = conn.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0]
        assert version >= 1

    # Run again: idempotency check
    db_mgr.initialize()
    with db_mgr.connection() as conn:
        count = conn.execute("SELECT COUNT(*) FROM schema_migrations WHERE version=1").fetchone()[0]
        assert count == 1

