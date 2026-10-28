"""SQLite connection and migration management with WAL mode and busy timeout."""

import sqlite3
import threading
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Generator


MIGRATION_V1 = """
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    credential_type TEXT NOT NULL,
    encrypted_payload BLOB NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_credentials_channel ON credentials(channel);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    client_id TEXT NOT NULL,
    encrypted_tokens BLOB NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    expires_at REAL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_channel ON oauth_tokens(channel);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    agent_id TEXT,
    channel TEXT,
    status TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp);
"""

MIGRATIONS = [
    (1, "init_vault_schema", MIGRATION_V1),
]


class DatabaseManager:
    """Manages SQLite WAL database connection lifecycle and schema migrations."""

    def __init__(self, db_path: str | Path = "clawbridge_vault.db", busy_timeout_ms: int = 5000):
        self.db_path = Path(db_path)
        self.busy_timeout_ms = busy_timeout_ms
        self._lock = threading.Lock()
        self._initialized = False

    def _configure_connection(self, conn: sqlite3.Connection) -> None:
        """Apply performance and concurrency PRAGMAs to SQLite connection."""
        conn.execute("PRAGMA foreign_keys = ON;")
        conn.execute(f"PRAGMA busy_timeout = {self.busy_timeout_ms};")
        conn.row_factory = sqlite3.Row

    def initialize(self) -> None:
        """Create database directory, enable WAL mode, and execute migrations."""
        with self._lock:
            if not self.db_path.parent.exists():
                self.db_path.parent.mkdir(parents=True, exist_ok=True)

            conn = sqlite3.connect(self.db_path, check_same_thread=False)
            try:
                # PRAGMA journal_mode must run outside transaction
                conn.execute("PRAGMA journal_mode = WAL;")
                self._configure_connection(conn)

                # Ensure migrations table exists
                conn.execute("""
                    CREATE TABLE IF NOT EXISTS schema_migrations (
                        version INTEGER PRIMARY KEY,
                        name TEXT NOT NULL,
                        applied_at TEXT NOT NULL
                    );
                """)
                conn.commit()

                # Run pending migrations
                applied_versions = {
                    row[0] for row in conn.execute("SELECT version FROM schema_migrations").fetchall()
                }

                for version, name, ddl in MIGRATIONS:
                    if version not in applied_versions:
                        with conn:
                            conn.executescript(ddl)
                            now = datetime.now(timezone.utc).isoformat()
                            conn.execute(
                                "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
                                (version, name, now),
                            )
                self._initialized = True
            finally:
                conn.close()

    @contextmanager
    def connection(self) -> Generator[sqlite3.Connection, None, None]:
        """Provide a configured SQLite connection in a context manager."""
        if not self._initialized:
            self.initialize()

        conn = sqlite3.connect(self.db_path, check_same_thread=False)
        self._configure_connection(conn)
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()
