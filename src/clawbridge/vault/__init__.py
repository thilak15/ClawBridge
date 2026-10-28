"""ClawBridge vault module."""
from clawbridge.vault.crypto import EnvelopeCipher, DecryptionError
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.single_flight import SingleFlightGroup
from clawbridge.vault.vault import (
    CredentialVault,
    CredentialNotFoundError,
    TokenExpiredError,
    StoredCredential,
    OAuthTokenRecord,
)

__all__ = [
    "EnvelopeCipher",
    "DecryptionError",
    "DatabaseManager",
    "SingleFlightGroup",
    "CredentialVault",
    "CredentialNotFoundError",
    "TokenExpiredError",
    "StoredCredential",
    "OAuthTokenRecord",
]
