from pathlib import Path
import httpx
import pytest
from mcp.server.mcpserver import MCPServer
from clawbridge.auth.authenticator import AgentAuthenticator
from clawbridge.auth.registry import AgentRegistry
from clawbridge.dispatch.dispatcher import EgressDispatcher
from clawbridge.dispatch.rate_limiter import TokenBucketLimiter
from clawbridge.mcp.server import create_mcp_server
from clawbridge.mcp.tools import ClawBridgeTools
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault


@pytest.fixture
def mcp_setup(tmp_path: Path):
    db_file = tmp_path / "test_mcp_server.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()
    cipher = EnvelopeCipher(EnvelopeCipher.generate_key())
    vault = CredentialVault(db_mgr, cipher)
    registry = AgentRegistry(db_mgr)
    auth = AgentAuthenticator(registry, vault)
    limiter = TokenBucketLimiter()
    return registry, vault, auth, limiter


@pytest.mark.asyncio
async def test_mcp_server_tool_discovery(mcp_setup):
    registry, vault, auth, limiter = mcp_setup
    client = httpx.AsyncClient(transport=httpx.MockTransport(lambda req: httpx.Response(200)))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(dispatcher)

    server = create_mcp_server(tools)
    assert isinstance(server, MCPServer)

    tool_list = await server.list_tools()
    tool_names = [t.name for t in tool_list]

    assert "slack_post_message" in tool_names
    assert "jira_create_issue" in tool_names
    assert "twitter_post_tweet" in tool_names
    assert "discord_send_message" in tool_names


@pytest.mark.asyncio
async def test_mcp_server_call_tool(mcp_setup):
    registry, vault, auth, limiter = mcp_setup
    agent, agent_token = registry.register_agent("MCP-Agent", ["slack:*"])
    tok_id = vault.store_oauth_token("slack", "app", {"access_token": "xoxb-mcp"})

    def mock_handler(req: httpx.Request):
        return httpx.Response(200, json={"ok": True, "ts": "123.456"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_handler))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(dispatcher, default_token_ids={"slack": tok_id})
    server = create_mcp_server(tools)

    res = await server.call_tool(
        "slack_post_message",
        {"agent_token": agent_token, "channel_id": "C999", "text": "Hello MCP!"},
    )
    assert not res.is_error
    assert len(res.content) > 0
    import json
    parsed = json.loads(res.content[0].text)
    assert parsed["status"] == "success"
    assert parsed["data"]["ok"] is True
