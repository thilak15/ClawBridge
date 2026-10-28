import json
import sqlite3
import time
from pathlib import Path
import pytest
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.single_flight import SingleFlightGroup
from clawbridge.vault.vault import CredentialVault, CredentialNotFoundError, TokenExpiredError


@pytest.fixture
def vault(tmp_path: Path):
    db_file = tmp_path / "test_vault.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    cipher = EnvelopeCipher(EnvelopeCipher.generate_key())
    sf = SingleFlightGroup()
    return CredentialVault(db_manager=db_mgr, cipher=cipher, single_flight=sf)


def test_store_and_retrieve_api_credential(vault: CredentialVault):
    cred_id = vault.store_credential(
        channel="jira",
        credential_type="api_token",
        payload={"email": "bot@company.com", "token": "jira_secret_token_abc"},
        metadata={"environment": "production"}
    )
    assert cred_id.startswith("cred_")
    
    cred = vault.get_credential(cred_id)
    assert cred.channel == "jira"
    assert cred.credential_type == "api_token"
    assert cred.payload["email"] == "bot@company.com"
    assert cred.payload["token"] == "jira_secret_token_abc"
    assert cred.metadata["environment"] == "production"


def test_plaintext_never_written_to_sqlite(vault: CredentialVault):
    raw_secret = "xoxb-ultra-secret-slack-token-never-leak"
    cred_id = vault.store_credential(
        channel="slack",
        credential_type="bot_token",
        payload={"token": raw_secret}
    )
    
    # Direct database inspection to verify zero plaintext leakage
    with vault.db_manager.connection() as conn:
        row = conn.execute("SELECT encrypted_payload FROM credentials WHERE id = ?", (cred_id,)).fetchone()
        assert row is not None
        raw_blob = bytes(row["encrypted_payload"])
        
        # Plaintext must NOT appear anywhere in the raw stored bytes
        assert raw_secret.encode("utf-8") not in raw_blob


def test_store_and_retrieve_oauth_token(vault: CredentialVault):
    token_id = vault.store_oauth_token(
        channel="twitter",
        client_id="x_client_123",
        token_data={
            "access_token": "tw_acc_111",
            "refresh_token": "tw_ref_222",
            "token_type": "bearer",
            "scope": "tweet.read tweet.write",
            "expires_in": 7200
        }
    )
    
    record = vault.get_oauth_token(token_id)
    assert record.channel == "twitter"
    assert record.access_token == "tw_acc_111"
    assert record.refresh_token == "tw_ref_222"
    assert record.is_expired is False


@pytest.mark.asyncio
async def test_concurrent_oauth_refresh_stampede_protection(vault: CredentialVault):
    # Store token that expired 10 seconds ago
    token_id = vault.store_oauth_token(
        channel="slack",
        client_id="slack_client_xyz",
        token_data={
            "access_token": "expired_acc_token",
            "refresh_token": "valid_refresh_token_789",
            "expires_at": time.time() - 10
        }
    )
    
    refresh_counter = 0
    
    async def mock_downstream_refresh(token_record):
        nonlocal refresh_counter
        refresh_counter += 1
        return {
            "access_token": f"refreshed_access_{refresh_counter}",
            "refresh_token": "next_refresh_token_abc",
            "expires_in": 3600
        }
    
    # 20 concurrent agent requests attempting to refresh the expired token
    import asyncio
    records = await asyncio.gather(*[
        vault.get_or_refresh_oauth_token(token_id, mock_downstream_refresh)
        for _ in range(20)
    ])
    
    # The refresh function was called exactly ONCE despite 20 callers
    assert refresh_counter == 1
    
    for rec in records:
        assert rec.access_token == "refreshed_access_1"
        assert rec.is_expired is False


def test_refresh_token_retained_if_omitted_in_update(vault: CredentialVault):
    token_id = vault.store_oauth_token(
        channel="github",
        client_id="gh_client",
        token_data={
            "access_token": "init_access_token",
            "refresh_token": "long_lived_refresh_token",
            "expires_in": 3600
        }
    )
    
    # Update with new access token, but omitting refresh_token (RFC 6749 §5.1)
    vault.store_oauth_token(
        channel="github",
        client_id="gh_client",
        token_data={
            "access_token": "new_access_token_only",
            "expires_in": 3600
        },
        token_id=token_id
    )
    
    updated = vault.get_oauth_token(token_id)
    assert updated.access_token == "new_access_token_only"
    # Crucial: original refresh token must NOT be lost/clobbered
    assert updated.refresh_token == "long_lived_refresh_token"


def test_audit_log_recorded(vault: CredentialVault):
    vault.record_audit_event(
        event_type="token_accessed",
        status="success",
        agent_id="nemoclaw-01",
        channel="slack",
        details={"action": "slack_post_message"}
    )
    
    with vault.db_manager.connection() as conn:
        row = conn.execute("SELECT * FROM audit_log WHERE agent_id = ?", ("nemoclaw-01",)).fetchone()
        assert row is not None
        assert row["channel"] == "slack"
        assert row["status"] == "success"
        details = json.loads(row["details_json"])
        assert details["action"] == "slack_post_message"

