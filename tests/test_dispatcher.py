from pathlib import Path
import httpx
import pytest
from clawbridge.auth.authenticator import AgentAuthenticator
from clawbridge.auth.registry import AgentRegistry
from clawbridge.dispatch.dispatcher import EgressDispatcher
from clawbridge.dispatch.errors import EgressReceipt
from clawbridge.dispatch.rate_limiter import TokenBucketLimiter, ChannelRate
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault


@pytest.fixture
def test_env(tmp_path: Path):
    db_file = tmp_path / "test_dispatch.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    cipher = EnvelopeCipher(EnvelopeCipher.generate_key())
    vault = CredentialVault(db_mgr, cipher)
    registry = AgentRegistry(db_mgr)
    auth = AgentAuthenticator(registry, vault)
    limiter = TokenBucketLimiter({"slack": ChannelRate(rate_per_second=10.0, capacity=10.0)})
    return registry, vault, auth, limiter


@pytest.mark.asyncio
async def test_successful_dispatch_with_bearer_token_injection(test_env):
    registry, vault, auth, limiter = test_env
    agent, agent_token = registry.register_agent("NemoClaw-Post", ["slack:post_message"])
    
    # Store Slack OAuth token in vault
    tok_id = vault.store_oauth_token(
        channel="slack",
        client_id="slack_bot",
        token_data={"access_token": "xoxb-real-slack-secret", "expires_in": 3600}
    )
    
    captured_request = None
    
    def mock_handler(request: httpx.Request):
        nonlocal captured_request
        captured_request = request
        return httpx.Response(200, json={"ok": True, "message_ts": "1700000000.001"})
        
    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_handler))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    
    receipt = await dispatcher.dispatch(
        bearer_header=f"Bearer {agent_token}",
        channel="slack",
        action="post_message",
        method="POST",
        url="https://slack.com/api/chat.postMessage",
        token_id=tok_id,
        json={"text": "Hello world from NemoClaw"}
    )
    
    assert receipt.status == "success"
    assert receipt.status_code == 200
    assert receipt.data == {"ok": True, "message_ts": "1700000000.001"}
    
    # Assert downstream call had JIT authorization attached
    assert captured_request is not None
    assert captured_request.headers["Authorization"] == "Bearer xoxb-real-slack-secret"
    
    # Assert raw downstream secret is NOT exposed in receipt
    assert "xoxb-real-slack-secret" not in str(receipt)


@pytest.mark.asyncio
async def test_dispatch_blocks_unauthorized_action(test_env):
    """Zero-Trust ACL boundary check: Agent has slack permissions but tries Jira."""
    registry, vault, auth, limiter = test_env
    agent, agent_token = registry.register_agent("SlackOnly-Agent", ["slack:*"])
    
    client = httpx.AsyncClient(transport=httpx.MockTransport(lambda req: httpx.Response(200)))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    
    receipt = await dispatcher.dispatch(
        bearer_header=f"Bearer {agent_token}",
        channel="jira",
        action="delete_issue",
        method="DELETE",
        url="https://jira.company.com/issue/123"
    )
    
    assert receipt.status == "error"
    assert receipt.code == "FORBIDDEN"
    assert receipt.retryable is False
    assert "not authorized" in receipt.message


@pytest.mark.asyncio
async def test_dispatch_handles_downstream_429_and_pauses_rate_limiter(test_env):
    registry, vault, auth, limiter = test_env
    agent, agent_token = registry.register_agent("Twitter-Bot", ["twitter:post"])
    tok_id = vault.store_oauth_token("twitter", "x_app", {"access_token": "tw_tok"})
    
    def mock_429_handler(request: httpx.Request):
        return httpx.Response(429, headers={"Retry-After": "10"}, json={"error": "Rate limit exceeded"})
        
    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_429_handler))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    
    receipt = await dispatcher.dispatch(
        bearer_header=f"Bearer {agent_token}",
        channel="twitter",
        action="post",
        method="POST",
        url="https://api.twitter.com/2/tweets",
        token_id=tok_id,
        json={"text": "Hi"}
    )
    
    assert receipt.status == "error"
    assert receipt.code == "RATE_LIMITED"
    assert receipt.retry_after_s == 10.0
    assert receipt.retryable is True
    
    # Verify limiter recorded downstream throttle
    assert limiter.is_available("twitter") is False

