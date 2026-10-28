"""Cryptographic HMAC signature verification, timestamp freshness check, and delivery deduplication."""

import hmac
import hashlib
import json
import sqlite3
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any
from clawbridge.vault.db import DatabaseManager


class WebhookVerificationError(Exception):
    """Raised when an inbound webhook fails cryptographic or freshness verification."""
    pass


@dataclass(frozen=True)
class VerifiedWebhook:
    """Represents a verified inbound webhook."""
    channel: str
    delivery_id: str
    payload: dict[str, Any]
    is_valid: bool = True


class WebhookVerifier:
    """Validates platform HMAC-SHA256 signatures, 300s freshness, and deduplicates delivery IDs in SQLite."""

    def __init__(self, db_manager: DatabaseManager, max_clock_skew_s: int = 300):
        self.db_manager = db_manager
        self.max_clock_skew_s = max_clock_skew_s

    def _check_and_record_delivery(self, channel: str, delivery_id: str, raw_body: bytes) -> None:
        """Deduplicate against SQLite webhook_events table, raising WebhookVerificationError on replay."""
        now = datetime.now(timezone.utc).isoformat()
        event_id = f"ev_{uuid.uuid4().hex[:16]}"
        try:
            payload_json = raw_body.decode("utf-8")
        except UnicodeDecodeError:
            payload_json = json.dumps({"raw_bytes": list(raw_body)})

        with self.db_manager.connection() as conn:
            try:
                conn.execute(
                    """
                    INSERT INTO webhook_events (id, channel, delivery_id, payload_json, status, received_at)
                    VALUES (?, ?, ?, ?, 'received', ?)
                    """,
                    (event_id, channel, delivery_id, payload_json, now),
                )
            except sqlite3.IntegrityError:
                raise WebhookVerificationError(
                    f"Duplicate webhook delivery ID '{delivery_id}' detected. Replay rejected."
                )

    def _parse_payload(self, body: bytes) -> dict[str, Any]:
        try:
            return json.loads(body.decode("utf-8"))
        except Exception:
            return {"raw": body.decode("utf-8", errors="replace")}

    def verify_slack(
        self,
        body: bytes,
        signature: str,
        timestamp_header: str | None,
        secret: str,
        delivery_id: str | None = None,
    ) -> VerifiedWebhook:
        """Verify Slack inbound webhook: v0=<hex>, timestamp within 300s, replay deduplication."""
        if not timestamp_header:
            raise WebhookVerificationError("Missing Slack timestamp header (X-Slack-Request-Timestamp)")

        try:
            ts_float = float(timestamp_header)
        except ValueError:
            raise WebhookVerificationError(f"Invalid timestamp header: {timestamp_header}")

        # Check 300-second freshness window
        now = time.time()
        if abs(now - ts_float) > self.max_clock_skew_s:
            raise WebhookVerificationError(
                f"Slack webhook timestamp {timestamp_header} is outside the {self.max_clock_skew_s}s freshness window."
            )

        # Signature verification: v0=HMAC-SHA256(secret, "v0:{timestamp}:{body}")
        sig_basestring = f"v0:{timestamp_header}:".encode("utf-8") + body
        computed_hash = hmac.new(secret.encode("utf-8"), sig_basestring, hashlib.sha256).hexdigest()
        expected_sig = f"v0={computed_hash}"

        if not hmac.compare_digest(signature, expected_sig):
            raise WebhookVerificationError("Slack HMAC-SHA256 signature mismatch.")

        # Deduplicate
        resolved_delivery_id = delivery_id or f"slack_{ts_float}_{computed_hash[:16]}"
        self._check_and_record_delivery("slack", resolved_delivery_id, body)

        return VerifiedWebhook(
            channel="slack",
            delivery_id=resolved_delivery_id,
            payload=self._parse_payload(body),
        )

    def verify_hmac_sha256(
        self,
        channel: str,
        body: bytes,
        signature_header: str,
        secret: str,
        delivery_id: str,
        prefix: str = "sha256=",
    ) -> VerifiedWebhook:
        """Verify generic / GitHub style HMAC-SHA256 signature with prefix."""
        if not signature_header:
            raise WebhookVerificationError(f"Missing signature header for channel '{channel}'")

        sig_val = signature_header
        if prefix and signature_header.startswith(prefix):
            sig_val = signature_header[len(prefix):]

        computed = hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()
        if not hmac.compare_digest(sig_val, computed):
            raise WebhookVerificationError(f"HMAC-SHA256 signature mismatch for channel '{channel}'.")

        self._check_and_record_delivery(channel, delivery_id, body)

        return VerifiedWebhook(
            channel=channel,
            delivery_id=delivery_id,
            payload=self._parse_payload(body),
        )

