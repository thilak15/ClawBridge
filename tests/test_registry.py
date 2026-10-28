from pathlib import Path
import pytest
from clawbridge.auth.registry import AgentRegistry, AgentNotFoundError
from clawbridge.vault.db import DatabaseManager


@pytest.fixture
def registry(tmp_path: Path):
    db_file = tmp_path / "test_auth_vault.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    return AgentRegistry(db_mgr)


def test_register_agent_returns_token_and_persists_hash(registry: AgentRegistry):
    record, raw_token = registry.register_agent(
        name="NemoClaw-Worker",
        allowed_actions=["slack:post_message", "jira:read_issue"]
    )
    
    assert record.id.startswith("agent_")
    assert record.name == "NemoClaw-Worker"
    assert record.is_active is True
    assert record.allowed_actions == ["slack:post_message", "jira:read_issue"]
    assert raw_token.startswith("clw_agent_")
    
    # Raw token should NOT be stored plaintext in SQLite
    with registry.db_manager.connection() as conn:
        row = conn.execute("SELECT * FROM agents WHERE id = ?", (record.id,)).fetchone()
        assert row is not None
        assert row["token_hash"] != raw_token
        assert row["status"] == "active"


def test_lookup_agent_by_raw_token(registry: AgentRegistry):
    record, raw_token = registry.register_agent(
        name="OpenClaw-Lead",
        allowed_actions=["*"]
    )
    
    found = registry.get_agent_by_token(raw_token)
    assert found is not None
    assert found.id == record.id
    assert found.name == "OpenClaw-Lead"


def test_lookup_unknown_token_returns_none(registry: AgentRegistry):
    assert registry.get_agent_by_token("clw_agent_nonexistent_token_12345") is None


def test_revoke_agent(registry: AgentRegistry):
    record, raw_token = registry.register_agent(
        name="Muse-Temporary",
        allowed_actions=["twitter:post"]
    )
    
    registry.revoke_agent(record.id)
    
    revoked_record = registry.get_agent(record.id)
    assert revoked_record.status == "revoked"
    assert revoked_record.is_active is False
    
    # Revoked agents should not resolve via token lookup
    assert registry.get_agent_by_token(raw_token) is None
