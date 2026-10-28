"""Cryptographically secure agent token generation and constant-time verification."""

import hashlib
import hmac
import secrets


class TokenManager:
    """Manages generation, hashing, and constant-time verification of Agent Identity tokens."""

    TOKEN_PREFIX = "clw_agent_"

    @classmethod
    def generate_token(cls, agent_id: str | None = None) -> tuple[str, str]:
        """Generate a random 256-bit agent bearer token and its SHA-256 storage hash.

        Returns:
            tuple[str, str]: (raw_bearer_token, sha256_hex_hash)
        """
        random_bytes = secrets.token_urlsafe(32)
        raw_token = f"{cls.TOKEN_PREFIX}{random_bytes}"
        token_hash = cls.hash_token(raw_token)
        return raw_token, token_hash

    @classmethod
    def hash_token(cls, raw_token: str) -> str:
        """Compute the deterministic SHA-256 hex digest of a bearer token."""
        return hashlib.sha256(raw_token.encode("utf-8")).hexdigest()

    @classmethod
    def verify_token(cls, raw_token: str, expected_hash: str) -> bool:
        """Verify token authenticity in constant time to prevent timing attacks."""
        computed_hash = cls.hash_token(raw_token)
        return hmac.compare_digest(computed_hash, expected_hash)
