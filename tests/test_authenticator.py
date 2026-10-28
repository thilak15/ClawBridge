from pathlib import Path
import pytest
from clawbridge.auth.authenticator import (
    AgentAuthenticator,
    AuthenticationError,
    PermissionDeniedError,
    AgentContext,
)
from clawbridge.auth.registry import AgentRegistry
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault


@pytest.fixture
def auth_stack(tmp_path: Path):
    db_file = tmp_path / "test_auth_stack.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    cipher = EnvelopeCipher(EnvelopeCipher.generate_key())
    vault = CredentialVault(db_mgr, cipher)
    registry = AgentRegistry(db_mgr)
    authenticator = AgentAuthenticator(registry, vault)
    return registry, vault, authenticator


def test_successful_authentication_and_authorization(auth_stack):
    registry, vault, authenticator = auth_stack
    agent, raw_token = registry.register_agent(
        name="NemoClaw-Reporter",
        allowed_actions=["slack:post_message"]
    )
    
    ctx = authenticator.authenticate_and_authorize(
        bearer_header=f"Bearer {raw_token}",
        channel="slack",
        action="post_message"
    )
    
    assert isinstance(ctx, AgentContext)
    assert ctx.agent_id == agent.id
    assert ctx.agent_name == "NemoClaw-Reporter"
    assert ctx.channel == "slack"
    assert ctx.action == "post_message"
    
    # Verify audit success entry was written
    with vault.db_manager.connection() as conn:
        row = conn.execute("SELECT * FROM audit_log WHERE event_type = 'auth_success'").fetchone()
        assert row is not None
        assert row["agent_id"] == agent.id


def test_missing_or_invalid_bearer_header(auth_stack):
    _, vault, authenticator = auth_stack
    
    with pytest.raises(AuthenticationError, match="Missing or malformed"):
        authenticator.authenticate_and_authorize(
            bearer_header=None,
            channel="slack",
            action="post_message"
        )
        
    with pytest.raises(AuthenticationError, match="Missing or malformed"):
        authenticator.authenticate_and_authorize(
            bearer_header="Basic 123456",
            channel="slack",
            action="post_message"
        )


def test_invalid_or_revoked_token_fails(auth_stack):
    registry, _, authenticator = auth_stack
    agent, raw_token = registry.register_agent(
        name="Muse-Revoked",
        allowed_actions=["twitter:post"]
    )
    
    registry.revoke_agent(agent.id)
    
    with pytest.raises(AuthenticationError, match="Invalid or revoked"):
        authenticator.authenticate_and_authorize(
            bearer_header=f"Bearer {raw_token}",
            channel="twitter",
            action="post"
        )


def test_privilege_escalation_blocked_by_acl(auth_stack):
    """Stress-test against [HIGH] Agent Privilege Escalation via Unscoped Tool Dispatch."""
    registry, vault, authenticator = auth_stack
    
    # Agent only permitted to read jira issues, attempts destructive action
    agent, raw_token = registry.register_agent(
        name="Low-Privilege-Bot",
        allowed_actions=["jira:read_issue", "slack:post_message"]
    )
    
    with pytest.raises(PermissionDeniedError, match="not authorized to invoke 'jira:delete_project'"):
        authenticator.authenticate_and_authorize(
            bearer_header=f"Bearer {raw_token}",
            channel="jira",
            action="delete_project"
        )
        
    # Check that denial was recorded in audit log
    with vault.db_manager.connection() as conn:
        row = conn.execute("SELECT * FROM audit_log WHERE event_type = 'permission_denied'").fetchone()
        assert row is not None
        assert row["agent_id"] == agent.id
        assert row["channel"] == "jira"
