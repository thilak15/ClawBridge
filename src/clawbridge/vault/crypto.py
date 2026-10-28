"""Envelope encryption for ClawBridge Credential Vault using AES-256-GCM."""

import os
from typing import Self
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF
from cryptography.hazmat.primitives import hashes
from cryptography.exceptions import InvalidTag


class DecryptionError(Exception):
    """Raised when ciphertext payload cannot be decrypted or authentication tag fails."""
    pass


class EnvelopeCipher:
    """AES-256-GCM envelope cipher for securing credentials and OAuth tokens at rest."""

    NONCE_SIZE = 12  # Standard 96-bit nonce for AES-GCM
    KEY_SIZE = 32    # 256-bit key

    def __init__(self, key: bytes):
        if len(key) != self.KEY_SIZE:
            raise ValueError(f"AES-256 key must be exactly {self.KEY_SIZE} bytes, got {len(key)}")
        self._key = key
        self._aesgcm = AESGCM(key)

    @classmethod
    def generate_key(cls) -> bytes:
        """Generate a cryptographically secure 256-bit random key."""
        return AESGCM.generate_key(bit_length=256)

    @classmethod
    def from_passphrase(cls, passphrase: str, salt: bytes = b"clawbridge-vault-salt-v1") -> Self:
        """Derive a deterministic 256-bit key from a passphrase using HKDF-SHA256."""
        hkdf = HKDF(
            algorithm=hashes.SHA256(),
            length=cls.KEY_SIZE,
            salt=salt,
            info=b"clawbridge-credential-vault-master-key",
        )
        derived_key = hkdf.derive(passphrase.encode("utf-8"))
        return cls(derived_key)

    def encrypt(self, plaintext: bytes, associated_data: bytes | None = None) -> bytes:
        """Encrypt plaintext bytes with a fresh random nonce.

        Returns:
            bytes: nonce (12 bytes) + ciphertext + 16-byte auth tag
        """
        nonce = os.urandom(self.NONCE_SIZE)
        ciphertext = self._aesgcm.encrypt(nonce, plaintext, associated_data)
        return nonce + ciphertext

    def decrypt(self, payload: bytes, associated_data: bytes | None = None) -> bytes:
        """Decrypt payload bytes, verifying the integrity tag.

        Raises:
            DecryptionError: if payload is too short, tampered with, or key is invalid.
        """
        if len(payload) < self.NONCE_SIZE + 16:  # 12-byte nonce + 16-byte tag
            raise DecryptionError("Ciphertext payload is too short to be valid AES-GCM")

        nonce = payload[:self.NONCE_SIZE]
        ciphertext = payload[self.NONCE_SIZE:]

        try:
            return self._aesgcm.decrypt(nonce, ciphertext, associated_data)
        except InvalidTag as exc:
            raise DecryptionError("Ciphertext authentication tag verification failed") from exc
        except Exception as exc:
            raise DecryptionError(f"Decryption failed: {exc}") from exc
