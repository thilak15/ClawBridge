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
    """AES-256-GCM envelope cipher using KEK to wrap ephemeral per-record DEKs."""

    NONCE_SIZE = 12  # Standard 96-bit nonce for AES-GCM
    KEY_SIZE = 32    # 256-bit key
    WRAPPED_DEK_SIZE = 12 + 32 + 16  # KEK nonce (12) + DEK (32) + AEAD tag (16)

    def __init__(self, key: bytes):
        if len(key) != self.KEY_SIZE:
            raise ValueError(f"AES-256 key must be exactly {self.KEY_SIZE} bytes, got {len(key)}")
        self._kek = key
        self._kek_cipher = AESGCM(key)

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
        """Envelope-encrypt plaintext: generate ephemeral DEK, wrap DEK with KEK, encrypt payload with DEK.

        Returns:
            bytes: wrapped_dek (60 bytes) + dek_nonce (12 bytes) + ciphertext + tag (16 bytes)
        """
        dek = AESGCM.generate_key(bit_length=256)
        kek_nonce = os.urandom(self.NONCE_SIZE)
        wrapped_dek = kek_nonce + self._kek_cipher.encrypt(kek_nonce, dek, None)

        dek_nonce = os.urandom(self.NONCE_SIZE)
        dek_cipher = AESGCM(dek)
        ciphertext = dek_cipher.encrypt(dek_nonce, plaintext, associated_data)

        return wrapped_dek + dek_nonce + ciphertext

    def decrypt(self, payload: bytes, associated_data: bytes | None = None) -> bytes:
        """Decrypt envelope payload: unwrap DEK with KEK, then decrypt payload with DEK.

        Raises:
            DecryptionError: if payload is tampered with, invalid key, or invalid tag.
        """
        if len(payload) < self.WRAPPED_DEK_SIZE + self.NONCE_SIZE + 16:
            raise DecryptionError("Ciphertext payload is too short to be valid envelope ciphertext")

        wrapped_dek = payload[:self.WRAPPED_DEK_SIZE]
        kek_nonce = wrapped_dek[:self.NONCE_SIZE]
        encrypted_dek = wrapped_dek[self.NONCE_SIZE:]

        try:
            dek = self._kek_cipher.decrypt(kek_nonce, encrypted_dek, None)
        except InvalidTag as exc:
            raise DecryptionError("KEK tag verification failed (wrong master key or tampered DEK)") from exc
        except Exception as exc:
            raise DecryptionError(f"Failed to unwrap DEK: {exc}") from exc

        dek_data = payload[self.WRAPPED_DEK_SIZE:]
        dek_nonce = dek_data[:self.NONCE_SIZE]
        ciphertext = dek_data[self.NONCE_SIZE:]

        try:
            dek_cipher = AESGCM(dek)
            return dek_cipher.decrypt(dek_nonce, ciphertext, associated_data)
        except InvalidTag as exc:
            raise DecryptionError("DEK payload authentication tag verification failed") from exc
        except Exception as exc:
            raise DecryptionError(f"Failed to decrypt payload with DEK: {exc}") from exc

