"""Agent Identity Registry for persistent agent credentials and permissions."""

import json
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone

from clawbridge.auth.tokens import TokenManager
from clawbridge.vault.db import DatabaseManager


class AgentNotFoundError(Exception):
    """Raised when a requested agent identity cannot be found."""
    pass


@dataclass(frozen=True)
class AgentRecord:
    """Represents an autonomous agent identity and its allowed action policies."""
    id: str
    name: str
    token_hash: str
    status: str
    allowed_actions: list[str]
    created_at: str
    updated_at: str

    @property
    def is_active(self) -> bool:
        return self.status == "active"


class AgentRegistry:
    """Manages agent registrations, token hashing, and status in SQLite."""

    def __init__(self, db_manager: DatabaseManager):
        self.db_manager = db_manager

    def register_agent(
        self,
        name: str,
        allowed_actions: list[str],
        agent_id: str | None = None,
    ) -> tuple[AgentRecord, str]:
        """Register a new autonomous agent and return its record plus the raw bearer token once."""
        agent_id = agent_id or f"agent_{uuid.uuid4().hex[:12]}"
        raw_token, token_hash = TokenManager.generate_token(agent_id)
        now = datetime.now(timezone.utc).isoformat()
        actions_json = json.dumps(allowed_actions)

        with self.db_manager.connection() as conn:
            conn.execute(
                """
                INSERT INTO agents (id, name, token_hash, status, allowed_actions_json, created_at, updated_at)
                VALUES (?, ?, ?, 'active', ?, ?, ?)
                """,
                (agent_id, name, token_hash, actions_json, now, now),
            )

        record = AgentRecord(
            id=agent_id,
            name=name,
            token_hash=token_hash,
            status="active",
            allowed_actions=allowed_actions,
            created_at=now,
            updated_at=now,
        )
        return record, raw_token

    def get_agent(self, agent_id: str) -> AgentRecord:
        """Fetch an agent record by ID."""
        with self.db_manager.connection() as conn:
            row = conn.execute("SELECT * FROM agents WHERE id = ?", (agent_id,)).fetchone()
            if not row:
                raise AgentNotFoundError(f"Agent '{agent_id}' not found")

            allowed_actions = json.loads(row["allowed_actions_json"])
            return AgentRecord(
                id=row["id"],
                name=row["name"],
                token_hash=row["token_hash"],
                status=row["status"],
                allowed_actions=allowed_actions,
                created_at=row["created_at"],
                updated_at=row["updated_at"],
            )

    def get_agent_by_token(self, raw_token: str) -> AgentRecord | None:
        """Resolve an active agent record from a raw bearer token."""
        token_hash = TokenManager.hash_token(raw_token)
        with self.db_manager.connection() as conn:
            row = conn.execute(
                "SELECT * FROM agents WHERE token_hash = ? AND status = 'active'",
                (token_hash,),
            ).fetchone()
            if not row:
                return None

            allowed_actions = json.loads(row["allowed_actions_json"])
            return AgentRecord(
                id=row["id"],
                name=row["name"],
                token_hash=row["token_hash"],
                status=row["status"],
                allowed_actions=allowed_actions,
                created_at=row["created_at"],
                updated_at=row["updated_at"],
            )

    def revoke_agent(self, agent_id: str) -> None:
        """Revoke an agent's access, invalidating its bearer token."""
        now = datetime.now(timezone.utc).isoformat()
        with self.db_manager.connection() as conn:
            cursor = conn.execute(
                "UPDATE agents SET status = 'revoked', updated_at = ? WHERE id = ?",
                (now, agent_id),
            )
            if cursor.rowcount == 0:
                raise AgentNotFoundError(f"Agent '{agent_id}' not found")
