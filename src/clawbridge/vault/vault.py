"""Credential Vault service: encrypted storage and single-flight token management."""

import json
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable

from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.single_flight import SingleFlightGroup


class CredentialNotFoundError(Exception):
    """Raised when a requested credential ID does not exist."""
    pass


class TokenExpiredError(Exception):
    """Raised when an OAuth token is expired and cannot be refreshed."""
    pass


@dataclass(frozen=True)
class StoredCredential:
    """Represents a decrypted credential entry retrieved from the vault."""
    id: str
    channel: str
    credential_type: str
    payload: dict[str, Any]
    metadata: dict[str, Any]
    created_at: str
    updated_at: str


@dataclass(frozen=True)
class OAuthTokenRecord:
    """Represents a decrypted OAuth token entry with expiry metadata."""
    id: str
    channel: str
    client_id: str
    access_token: str
    refresh_token: str | None
    token_type: str
    scope: str
    expires_at: float | None
    created_at: str
    updated_at: str

    @property
    def is_expired(self) -> bool:
        """Check if token is expired with a 30-second pre-expiry buffer."""
        if self.expires_at is None:
            return False
        return time.time() >= (self.expires_at - 30.0)


class CredentialVault:
    """Unified encrypted store for agent credentials and OAuth tokens with single-flight refresh."""

    def __init__(
        self,
        db_manager: DatabaseManager,
        cipher: EnvelopeCipher,
        single_flight: SingleFlightGroup | None = None,
    ):
        self.db_manager = db_manager
        self.cipher = cipher
        self.single_flight = single_flight or SingleFlightGroup()

    def store_credential(
        self,
        channel: str,
        credential_type: str,
        payload: dict[str, Any],
        metadata: dict[str, Any] | None = None,
        cred_id: str | None = None,
    ) -> str:
        """Encrypt and store an arbitrary credential (API key, bot token, secret)."""
        cred_id = cred_id or f"cred_{uuid.uuid4().hex[:16]}"
        now = datetime.now(timezone.utc).isoformat()
        metadata_json = json.dumps(metadata or {})

        payload_bytes = json.dumps(payload).encode("utf-8")
        encrypted_payload = self.cipher.encrypt(payload_bytes)

        with self.db_manager.connection() as conn:
            conn.execute(
                """
                INSERT INTO credentials (id, channel, credential_type, encrypted_payload, metadata_json, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(id) DO UPDATE SET
                    encrypted_payload = excluded.encrypted_payload,
                    metadata_json = excluded.metadata_json,
                    updated_at = excluded.updated_at
                """,
                (cred_id, channel, credential_type, encrypted_payload, metadata_json, now, now),
            )
        return cred_id

    def get_credential(self, cred_id: str) -> StoredCredential:
        """Retrieve and decrypt a credential by ID."""
        with self.db_manager.connection() as conn:
            row = conn.execute("SELECT * FROM credentials WHERE id = ?", (cred_id,)).fetchone()
            if not row:
                raise CredentialNotFoundError(f"Credential '{cred_id}' not found")

            decrypted_bytes = self.cipher.decrypt(bytes(row["encrypted_payload"]))
            payload = json.loads(decrypted_bytes.decode("utf-8"))
            metadata = json.loads(row["metadata_json"])

            return StoredCredential(
                id=row["id"],
                channel=row["channel"],
                credential_type=row["credential_type"],
                payload=payload,
                metadata=metadata,
                created_at=row["created_at"],
                updated_at=row["updated_at"],
            )

    def store_oauth_token(
        self,
        channel: str,
        client_id: str,
        token_data: dict[str, Any],
        token_id: str | None = None,
    ) -> str:
        """Encrypt and store an OAuth token bundle (access token, refresh token, expiry)."""
        token_id = token_id or f"tok_{uuid.uuid4().hex[:16]}"
        now = datetime.now(timezone.utc).isoformat()

        # Calculate absolute expiration timestamp if expires_in is provided
        expires_at = token_data.get("expires_at")
        if expires_at is None and "expires_in" in token_data:
            expires_at = time.time() + float(token_data["expires_in"])

        token_payload = {
            "access_token": token_data.get("access_token", ""),
            "refresh_token": token_data.get("refresh_token"),
            "token_type": token_data.get("token_type", "Bearer"),
        }
        encrypted_tokens = self.cipher.encrypt(json.dumps(token_payload).encode("utf-8"))
        scope = token_data.get("scope", "")

        with self.db_manager.connection() as conn:
            conn.execute(
                """
                INSERT INTO oauth_tokens (id, channel, client_id, encrypted_tokens, scope, expires_at, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(id) DO UPDATE SET
                    encrypted_tokens = excluded.encrypted_tokens,
                    scope = excluded.scope,
                    expires_at = excluded.expires_at,
                    updated_at = excluded.updated_at
                """,
                (token_id, channel, client_id, encrypted_tokens, scope, expires_at, now, now),
            )
        return token_id

    def get_oauth_token(self, token_id: str) -> OAuthTokenRecord:
        """Retrieve and decrypt an OAuth token record by ID."""
        with self.db_manager.connection() as conn:
            row = conn.execute("SELECT * FROM oauth_tokens WHERE id = ?", (token_id,)).fetchone()
            if not row:
                raise CredentialNotFoundError(f"OAuth token '{token_id}' not found")

            decrypted_bytes = self.cipher.decrypt(bytes(row["encrypted_tokens"]))
            token_payload = json.loads(decrypted_bytes.decode("utf-8"))

            return OAuthTokenRecord(
                id=row["id"],
                channel=row["channel"],
                client_id=row["client_id"],
                access_token=token_payload["access_token"],
                refresh_token=token_payload.get("refresh_token"),
                token_type=token_payload.get("token_type", "Bearer"),
                scope=row["scope"],
                expires_at=row["expires_at"],
                created_at=row["created_at"],
                updated_at=row["updated_at"],
            )

    async def get_or_refresh_oauth_token(
        self,
        token_id: str,
        refresh_fn: Callable[[OAuthTokenRecord], Awaitable[dict[str, Any]]],
    ) -> OAuthTokenRecord:
        """Return a valid access token, executing single-flight refresh under concurrency if expired."""
        current = self.get_oauth_token(token_id)
        if not current.is_expired:
            return current

        # Single-flight deduplication key per token record to prevent refresh stampedes
        async def _do_refresh() -> OAuthTokenRecord:
            # Re-read token inside single-flight to check if a previous concurrent caller refreshed it
            latest = self.get_oauth_token(token_id)
            if not latest.is_expired:
                return latest

            new_token_data = await refresh_fn(latest)
            self.store_oauth_token(
                channel=latest.channel,
                client_id=latest.client_id,
                token_data=new_token_data,
                token_id=token_id,
            )
            return self.get_oauth_token(token_id)

        return await self.single_flight.execute(f"oauth_refresh:{token_id}", _do_refresh)

    def record_audit_event(
        self,
        event_type: str,
        status: str,
        agent_id: str | None = None,
        channel: str | None = None,
        details: dict[str, Any] | None = None,
    ) -> None:
        """Append an immutable audit entry to the SQLite audit log."""
        now = datetime.now(timezone.utc).isoformat()
        details_json = json.dumps(details or {})

        with self.db_manager.connection() as conn:
            conn.execute(
                """
                INSERT INTO audit_log (event_type, agent_id, channel, status, timestamp, details_json)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (event_type, agent_id, channel, status, now, details_json),
            )
