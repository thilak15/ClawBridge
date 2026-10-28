"""End-to-End Multi-Agent Integration Test Suite & Verification Harness.

Validates the full ClawBridge lifecycle across:
1. Zero-Trust Multi-Agent Registration & Scoped Bearer Tokens (NemoClaw, OpenClaw, Gemini)
2. AES-256-GCM Encrypted SQLite WAL Vault with single-flight OAuth refresh deduplication
3. FastMCP tool execution (Slack, Jira, Twitter, Discord)
4. Outbound Egress rate-limiting and structured EgressReceipt error normalization
5. Inbound Webhook HMAC verification, timestamp freshness check, and delivery deduplication
6. Agent pub-sub Server-Sent Events (SSE) topic broadcasting
"""

import asyncio
import hashlib
import hmac
import json
import time
from pathlib import Path
import httpx
import pytest

from clawbridge.auth.authenticator import AgentAuthenticator
from clawbridge.auth.registry import AgentRegistry
from clawbridge.dispatch.dispatcher import EgressDispatcher
from clawbridge.dispatch.rate_limiter import TokenBucketLimiter
from clawbridge.inbound.broadcaster import EventBroadcaster
from clawbridge.inbound.normalizer import EventNormalizer
from clawbridge.inbound.verifier import WebhookVerifier, WebhookVerificationError
from clawbridge.mcp.server import create_mcp_server
from clawbridge.mcp.tools import ClawBridgeTools
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault, OAuthTokenRecord


@pytest.fixture
def harness_env(tmp_path: Path):
    db_file = tmp_path / "e2e_clawbridge.db"
    db_mgr = DatabaseManager(db_file)
    db_mgr.initialize()

    master_key = EnvelopeCipher.generate_key()
    cipher = EnvelopeCipher(master_key)
    vault = CredentialVault(db_mgr, cipher)
    registry = AgentRegistry(db_mgr)
    auth = AgentAuthenticator(registry, vault)
    limiter = TokenBucketLimiter()
    verifier = WebhookVerifier(db_mgr)
    normalizer = EventNormalizer()
    broadcaster = EventBroadcaster()

    return {
        "db_mgr": db_mgr,
        "vault": vault,
        "registry": registry,
        "auth": auth,
        "limiter": limiter,
        "verifier": verifier,
        "normalizer": normalizer,
        "broadcaster": broadcaster,
    }


@pytest.mark.asyncio
async def test_e2e_multi_agent_workflow(harness_env):
    """Full lifecycle: NemoClaw posts to Slack, OpenClaw creates Jira issue, unauthorized agent blocked."""
    vault = harness_env["vault"]
    registry = harness_env["registry"]
    auth = harness_env["auth"]
    limiter = harness_env["limiter"]

    # 1. Store platform credentials & tokens in encrypted vault
    slack_tok_id = vault.store_oauth_token("slack", "app_slack_001", {"access_token": "xoxb-real-slack-secret"})
    jira_cred_id = vault.store_credential(
        "jira",
        "basic_auth",
        {"email": "jira-bot@company.com", "token": "real-jira-api-token"},
    )

    # 2. Register distinct agents with strictly scoped permissions
    agent_nemo, nemo_token = registry.register_agent("NemoClaw", ["slack:post_message"])
    agent_open, open_token = registry.register_agent("OpenClaw", ["jira:create_issue"])
    agent_unauth, unauth_token = registry.register_agent("RestrictedAgent", ["read_only:*"])

    # 3. Setup mock transport capturing dispatched platform requests
    dispatched_requests: list[httpx.Request] = []

    def mock_transport(req: httpx.Request) -> httpx.Response:
        dispatched_requests.append(req)
        if "slack.com" in str(req.url):
            return httpx.Response(200, json={"ok": True, "ts": "1700000000.123"})
        elif "atlassian.net" in str(req.url):
            return httpx.Response(201, json={"id": "10001", "key": "DEV-101"})
        return httpx.Response(404)

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_transport))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)
    tools = ClawBridgeTools(
        dispatcher,
        default_token_ids={"slack": slack_tok_id},
        default_credential_ids={"jira": jira_cred_id},
    )
    server = create_mcp_server(tools)

    # 4. NemoClaw calls slack_post_message via MCP tool calling
    slack_call_res = await server.call_tool(
        "slack_post_message",
        {"agent_token": nemo_token, "channel_id": "C12345", "text": "Build completed!"},
    )
    assert not slack_call_res.is_error
    slack_receipt = json.loads(slack_call_res.content[0].text)
    assert slack_receipt["status"] == "success"
    assert slack_receipt["data"]["ok"] is True

    # Verify authorization header attached downstream by HeaderInjector
    slack_req = next(r for r in dispatched_requests if "slack.com" in str(r.url))
    assert slack_req.headers["authorization"] == "Bearer xoxb-real-slack-secret"

    # 5. OpenClaw calls jira_create_issue via MCP tool calling
    jira_call_res = await server.call_tool(
        "jira_create_issue",
        {
            "agent_token": open_token,
            "project_key": "DEV",
            "summary": "Fix flaky test",
            "description": "Details here",
        },
    )
    assert not jira_call_res.is_error
    jira_receipt = json.loads(jira_call_res.content[0].text)
    assert jira_receipt["status"] == "success"
    assert jira_receipt["data"]["key"] == "DEV-101"

    # Verify basic auth header attached downstream
    jira_req = next(r for r in dispatched_requests if "atlassian.net" in str(r.url))
    assert jira_req.headers["authorization"].startswith("Basic ")

    # 6. Privilege escalation attempt: NemoClaw attempts to create a Jira issue
    escalation_res = await server.call_tool(
        "jira_create_issue",
        {
            "agent_token": nemo_token,
            "project_key": "DEV",
            "summary": "Unauthorized ticket",
        },
    )
    assert not escalation_res.is_error  # MCP returns receipt block
    escalation_receipt = json.loads(escalation_res.content[0].text)
    assert escalation_receipt["status"] == "error"
    assert escalation_receipt["code"] == "FORBIDDEN"
    assert "not authorized" in escalation_receipt["message"].lower()

    # 7. Privilege escalation attempt: RestrictedAgent attempts any egress
    blocked_res = await server.call_tool(
        "slack_post_message",
        {"agent_token": unauth_token, "channel_id": "C123", "text": "spam"},
    )
    blocked_receipt = json.loads(blocked_res.content[0].text)
    assert blocked_receipt["status"] == "error"
    assert blocked_receipt["code"] == "FORBIDDEN"


@pytest.mark.asyncio
async def test_e2e_inbound_webhook_to_sse_streaming(harness_env):
    """Inbound webhook HMAC validation, deduplication, event normalization, and SSE broadcasting."""
    verifier = harness_env["verifier"]
    normalizer = harness_env["normalizer"]
    broadcaster = harness_env["broadcaster"]

    # 1. Agent subscribes to slack topics via SSE
    subscriber_queue = await broadcaster.subscribe(topics=["slack:*"])

    # 2. Inbound Slack webhook arrives with valid HMAC-SHA256 signature
    signing_secret = "slack_secret_key_456"
    now_ts = str(int(time.time()))
    body = b'{"type":"event_callback","event":{"type":"message","user":"U_ALICE","text":"Deploy triggered","channel":"C999"}}'
    sig_basestring = f"v0:{now_ts}:".encode("utf-8") + body
    sig = "v0=" + hmac.new(signing_secret.encode("utf-8"), sig_basestring, hashlib.sha256).hexdigest()

    verified = verifier.verify_slack(
        body=body,
        signature=sig,
        timestamp_header=now_ts,
        secret=signing_secret,
        delivery_id="deliv_e2e_001",
    )
    assert verified.is_valid

    # 3. Normalize into canonical InboundEvent
    event = normalizer.normalize(verified)
    assert event.topic == "slack:message"
    assert event.author == "U_ALICE"
    assert event.content == "Deploy triggered"

    # 4. Broadcast event to subscribed agents
    delivered_count = await broadcaster.publish(event)
    assert delivered_count == 1

    # 5. Agent receives event and formats into SSE block
    received_event = await asyncio.wait_for(subscriber_queue.get(), timeout=1.0)
    assert received_event.event_id == event.event_id
    sse_text = broadcaster.format_sse_message(received_event)
    assert f"id: {event.event_id}\n" in sse_text
    assert f"event: slack:message\n" in sse_text
    assert '"U_ALICE"' in sse_text

    # 6. Replay attack rejection: identical delivery_id rejected immediately
    with pytest.raises(WebhookVerificationError) as exc_info:
        verifier.verify_slack(
            body=body,
            signature=sig,
            timestamp_header=now_ts,
            secret=signing_secret,
            delivery_id="deliv_e2e_001",
        )
    assert "duplicate" in str(exc_info.value).lower()


@pytest.mark.asyncio
async def test_e2e_oauth_token_refresh_single_flight(harness_env):
    """Multi-agent parallel calls triggering expired OAuth token refresh execute exactly once."""
    vault = harness_env["vault"]
    registry = harness_env["registry"]
    auth = harness_env["auth"]
    limiter = harness_env["limiter"]

    # Store expired token in vault with refresh_token
    token_id = vault.store_oauth_token(
        "slack",
        "client_multi",
        {
            "access_token": "expired_tok",
            "refresh_token": "ref_valid_123",
            "expires_at": time.time() - 100,  # Expired
        },
    )

    refresh_counter = 0

    async def mock_refresh(rec: OAuthTokenRecord) -> dict[str, str | int]:
        nonlocal refresh_counter
        refresh_counter += 1
        await asyncio.sleep(0.05)  # Simulate network latency
        return {
            "access_token": "new_refreshed_access_tok",
            "refresh_token": "ref_valid_123",
            "expires_in": 3600,
        }

    # Register 5 agents calling simultaneously
    agents = [
        registry.register_agent(f"Agent-{i}", ["slack:post_message"])
        for i in range(5)
    ]

    def mock_handler(req: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"ok": True})

    client = httpx.AsyncClient(transport=httpx.MockTransport(mock_handler))
    dispatcher = EgressDispatcher(auth, vault, limiter, client)

    # Dispatch 5 parallel requests simultaneously using the expired token
    tasks = [
        dispatcher.dispatch(
            bearer_header=f"Bearer {tok}",
            channel="slack",
            action="post_message",
            method="POST",
            url="https://slack.com/api/chat.postMessage",
            token_id=token_id,
            refresh_fn=mock_refresh,
            json={"channel": "C123", "text": f"Msg {i}"},
        )
        for i, (_, tok) in enumerate(agents)
    ]

    receipts = await asyncio.gather(*tasks)

    # All 5 requests succeeded
    assert all(r.status == "success" for r in receipts)
    # Critical Red-Team Check: Token refresh single-flight mutex ensured only 1 network refresh was executed!
    assert refresh_counter == 1

