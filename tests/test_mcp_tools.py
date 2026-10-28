from pathlib import Path
import httpx
import pytest
from clawbridge.auth.authenticator import AgentAuthenticator
from clawbridge.auth.registry import AgentRegistry
from clawbridge.dispatch.dispatcher import EgressDispatcher
from clawbridge.dispatch.rate_limiter import TokenBucketLimiter
from clawbridge.mcp.tools import ClawBridgeTools
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault


@pytest.fixture
def test_setup(tmp_path: Path):
    db_file = tmp_path / "test_mcp.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    cipher = EnvelopeCipher(EnvelopeCipher.generate_key())
    vault = CredentialVault(db_mgr, cipher)
    registry = AgentRegistry(db_mgr)
    auth = AgentAuthenticator(registry, vault)
    limiter = TokenBucketLimiter()
    return registry, vault, auth, limiter


@pytest.mark.asyncio
async def test_slack_post_message_success(test_setup):
    registry, vault, auth, limiter = test_setup
    agent, agent_token = registry.register_agent("NemoClaw-Slack", ["slack:post_message"])
    tok_id = vault.store_oauth_token("slack", "slack_app", {"access_token": "xoxb-valid"})

    def mock_slack(req: httpx.Request):
        return httpx.Response(200, json={"ok": True, "ts": "1700000001.000"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_slack))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(dispatcher, default_token_ids={"slack": tok_id})

    res = await tools.slack_post_message(
        agent_token=agent_token,
        channel_id="C12345",
        text="Deploying to production!"
    )
    assert res["status"] == "success"
    assert res["status_code"] == 200
    assert res["data"] == {"ok": True, "ts": "1700000001.000"}


@pytest.mark.asyncio
async def test_jira_create_issue_success(test_setup):
    registry, vault, auth, limiter = test_setup
    agent, agent_token = registry.register_agent("OpenClaw-Jira", ["jira:create_issue"])
    cred_id = vault.store_credential("jira", "basic_auth", {"email": "bot@corp.com", "token": "jira_tok"})

    def mock_jira(req: httpx.Request):
        return httpx.Response(201, json={"id": "10001", "key": "PROJ-42"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_jira))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(dispatcher, default_credential_ids={"jira": cred_id})

    res = await tools.jira_create_issue(
        agent_token=agent_token,
        project_key="PROJ",
        summary="Automated bug report",
        description="Stack trace details here"
    )
    assert res["status"] == "success"
    assert res["status_code"] == 201
    assert res["data"]["key"] == "PROJ-42"


@pytest.mark.asyncio
async def test_privilege_escalation_blocked_via_mcp_tool(test_setup):
    """Stress-test against [HIGH] Agent Privilege Escalation: Agent with Slack only calling Jira."""
    registry, vault, auth, limiter = test_setup
    agent, agent_token = registry.register_agent("SlackOnly-Agent", ["slack:*"])

    client = httpx.AsyncClient(transport=httpx.MockTransport(lambda req: httpx.Response(200)))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(dispatcher)

    res = await tools.jira_create_issue(
        agent_token=agent_token,
        project_key="SEC",
        summary="Unauthorized ticket"
    )
    assert res["status"] == "error"
    assert res["code"] == "FORBIDDEN"
    assert "not authorized" in res["message"]


@pytest.mark.asyncio
async def test_twitter_and_discord_tool_handlers(test_setup):
    registry, vault, auth, limiter = test_setup
    agent, agent_token = registry.register_agent("Omni-Agent", ["*"])
    tok_id = vault.store_oauth_token("twitter", "tw_app", {"access_token": "tw_tok"})
    cred_id = vault.store_credential("discord", "bot_token", {"token": "disc_bot_tok"})

    def mock_handler(req: httpx.Request):
        if "twitter" in str(req.url):
            return httpx.Response(201, json={"data": {"id": "tweet_123", "text": "Hello!"}})
        return httpx.Response(200, json={"id": "msg_456", "content": "Discord msg"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_handler))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(
        dispatcher,
        default_token_ids={"twitter": tok_id},
        default_credential_ids={"discord": cred_id}
    )

    tw_res = await tools.twitter_post_tweet(agent_token=agent_token, text="Hello world!")
    assert tw_res["status"] == "success"
    assert tw_res["data"]["data"]["id"] == "tweet_123"

    disc_res = await tools.discord_send_message(agent_token=agent_token, channel_id="ch_99", content="Hello!")
    assert disc_res["status"] == "success"
    assert disc_res["data"]["id"] == "msg_456"
