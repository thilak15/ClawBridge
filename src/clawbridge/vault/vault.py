"""Credential Vault service: encrypted storage and single-flight token management."""

import json
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable

from authlib.oauth2.rfc6749 import OAuth2Token  # type: ignore[import-untyped]

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
    """Represents a decrypted OAuth token entry backed by Authlib OAuth2Token."""
    id: str
    channel: str
    client_id: str
    token: OAuth2Token
    created_at: str
    updated_at: str

    @property
    def access_token(self) -> str:
        return str(self.token.get("access_token", ""))

    @property
    def refresh_token(self) -> str | None:
        return self.token.get("refresh_token")

    @property
    def token_type(self) -> str:
        return str(self.token.get("token_type", "Bearer"))

    @property
    def scope(self) -> str:
        return str(self.token.get("scope", ""))

    @property
    def expires_at(self) -> float | None:
        return self.token.get("expires_at")

    @property
    def is_expired(self) -> bool:
        """Check if token is expired using Authlib OAuth2Token logic with a 30s buffer."""
        expires_at = self.token.get("expires_at")
        if expires_at is None:
            return False
        return time.time() >= (float(expires_at) - 30.0)


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
        """Encrypt and store an OAuth token bundle, preserving existing refresh token if omitted."""
        token_id = token_id or f"tok_{uuid.uuid4().hex[:16]}"
        now = datetime.now(timezone.utc).isoformat()

        # If updating an existing token and new refresh_token is omitted, preserve current
        existing_refresh_token = None
        if token_id:
            try:
                existing = self.get_oauth_token(token_id)
                existing_refresh_token = existing.refresh_token
            except CredentialNotFoundError:
                pass

        # Construct Authlib OAuth2Token
        token_dict = dict(token_data)
        if not token_dict.get("refresh_token") and existing_refresh_token:
            token_dict["refresh_token"] = existing_refresh_token

        authlib_token = OAuth2Token(token_dict)
        expires_at = authlib_token.get("expires_at")
        if expires_at is None and "expires_in" in authlib_token:
            expires_at = time.time() + float(authlib_token["expires_in"])
            authlib_token["expires_at"] = expires_at

        encrypted_tokens = self.cipher.encrypt(json.dumps(dict(authlib_token)).encode("utf-8"))
        scope = authlib_token.get("scope", "")

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
            raw_token = json.loads(decrypted_bytes.decode("utf-8"))
            authlib_token = OAuth2Token(raw_token)

            return OAuthTokenRecord(
                id=row["id"],
                channel=row["channel"],
                client_id=row["client_id"],
                token=authlib_token,
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

        if not current.refresh_token:
            raise TokenExpiredError(f"OAuth token '{token_id}' is expired and has no refresh token")

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

