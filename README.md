# ClawBridge

Zero-Trust Agent Egress Gateway and Inbound Event Adapter for Autonomous AI Agents.

---

## Overview

ClawBridge is a security-first egress broker, credential isolation vault, and bidirectional event adapter designed for multi-agent autonomous AI environments (such as NemoClaw, Muse, OpenClaw, and Gemini agents).

### The Problem

When autonomous agents interact with external platforms (Slack, Jira, GitHub, Discord, X/Twitter), granting them direct access to production API keys or OAuth refresh tokens introduces severe risks:

- **Prompt Injection & Secret Exfiltration:** Malicious instructions in prompt contexts or uncurated external inputs can trick LLMs into exposing raw keys or session credentials.
- **Context Leaks:** API keys, access tokens, and sensitive headers easily leak into conversation history, token logs, telemetry, and debugging artifacts.
- **Uncontrolled Blast Radius:** Direct access lacks centralized rate limiting, per-agent action whitelisting, or audit trails, risking cascading API bans and unintentional service outages.

### The ClawBridge Solution

ClawBridge decouples agent intelligence from credentials using a Zero-Trust architecture:

1. **Scoped Agent Identities:** Agents authenticate to ClawBridge using dedicated, scoped Agent Bearer Tokens (`clw_agent_...`).
2. **Zero-Trust Egress Proxy:** Agents issue outbound requests or tool calls through ClawBridge without ever seeing downstream secrets. ClawBridge evaluates permissions, pulls credentials from an encrypted vault, attaches headers just-in-time, and dispatches the request.
3. **Embedded Encrypted Vault:** Platform OAuth tokens and API secrets are stored locally in SQLite WAL mode using AES-256-GCM envelope encryption, with single-flight deduplication for automatic token refresh cycles.
4. **Resilience & Rate Limiting:** Internal token-bucket rate limiters prevent downstream API bans, converting errors into structured, machine-readable JSON receipts with retry metadata.
5. **Bidirectional Event Gateway:** Downstream webhooks are verified cryptographically (HMAC-SHA256, clock-skew checks, and replay deduplication) and normalized into an asynchronous pub-sub Server-Sent Events (SSE) stream.

---

## Architecture

```
+-----------------------------------------------------------------------------+
|                               Autonomous AI Agents                          |
|                       (NemoClaw, OpenClaw, Gemini, etc.)                    |
+-----------------------------------------------------------------------------+
               |                                            ^
  Agent Bearer | Outbound Tool Calls           Inbound SSE  | Normalized
  Token & ACL  | (MCP / REST)                  Stream       | Events
               v                                            |
+-----------------------------------------------------------------------------+
|                                  ClawBridge                                 |
|                                                                             |
|  +-------------------------+            +--------------------------------+  |
|  |   Agent Authenticator   |            |       Event Broadcaster        |  |
|  |    & Policy Engine      |            |      (Pub-Sub SSE Topics)      |  |
|  +-------------------------+            +--------------------------------+  |
|               |                                           ^                 |
|  +-------------------------+            +--------------------------------+  |
|  |   Token Bucket Limiter  |            |        Event Normalizer        |  |
|  | (Per-Channel Rate/Burst)|            |  (Slack, Jira, Generic Schema) |  |
|  +-------------------------+            +--------------------------------+  |
|               |                                           ^                 |
|  +-------------------------+            +--------------------------------+  |
|  |    Egress Dispatcher    |            |        Webhook Verifier        |  |
|  | (JIT Header Injection)  |            |   (HMAC-SHA256, Replay Cache)  |  |
|  +-------------------------+            +--------------------------------+  |
|               |                                           ^                 |
|  +------------------------------------------------------+ |                 |
|  |             Encrypted Vault (SQLite WAL)             | |                 |
|  |   AES-256-GCM Master Key + SingleFlight Refresh      | |                 |
|  +------------------------------------------------------+ |                 |
+-----------------------------------------------------------+-----------------+
               |                                            ^
  Outbound API | Authenticated               Inbound Webhook| Platform
  Requests     | HTTP Calls                  Payloads       | Signatures
               v                                            |
+-----------------------------------------------------------------------------+
|                             Downstream Platforms                            |
|                       (Slack, Jira, Twitter, Discord)                       |
+-----------------------------------------------------------------------------+
```

---

## Key Features

- **Zero-Trust Credential Isolation:** Raw API keys, basic auth secrets, and OAuth refresh tokens never reach agent contexts or LLM prompts.
- **Embedded AES-256-GCM Vault:** Backed by SQLite in WAL (Write-Ahead Logging) mode. All credentials and OAuth secrets are encrypted at rest with high-entropy nonces.
- **Single-Flight OAuth Refresh:** When multiple concurrent agent requests encounter an expired token, the refresh operation is deduplicated so the downstream OAuth provider is called exactly once.
- **Fine-Grained Agent Identity and ACLs:** Define exact permission scopes per agent (e.g., `slack:post_message`, `jira:read*`, `*`). Requests violating policy are rejected immediately.
- **Per-Channel Token Bucket Rate Limiting:** Enforces configurable burst capacities and replenishment rates per platform. Automatically applies backoff windows when downstream platforms return HTTP 429.
- **Normalized Execution Receipts:** Outbound actions return a unified `EgressReceipt` with status codes, machine-readable error codes (`UNAUTHORIZED`, `RATE_LIMITED`, `DOWNSTREAM_ERROR`), and `retry_after_s` metadata.
- **Model Context Protocol (MCP) Integration:** Exposes normalized tools (`slack_post_message`, `jira_create_issue`, `twitter_post_tweet`, `discord_send_message`) directly through an MCPServer instance.
- **Cryptographic Webhook Verification:** Verifies inbound signatures (Slack `v0=...`, GitHub / generic `sha256=...`), enforces a 300-second freshness window against clock skew, and deduplicates delivery IDs in SQLite to block replay attacks.
- **Asynchronous Inbound SSE Broadcaster:** Dispatches normalized inbound events (`InboundEvent`) to listening agents matching topic subscriptions and wildcards.

---

## Installation

### Prerequisites

- Python 3.11 or higher
- `uv` (recommended) or standard `pip`

### Install with uv

Clone the repository and install dependencies:

```bash
git clone https://github.com/thilak15/ClawBridge.git
cd ClawBridge
uv sync
```

### Install with pip / venv

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e .
```

---

## Quickstart Guide

### 1. Initialize the Database and Vault

ClawBridge stores encrypted secrets and event logs in a local SQLite database. Envelope encryption uses an AES-256 key:

```python
from pathlib import Path
from clawbridge.vault.crypto import EnvelopeCipher
from clawbridge.vault.db import DatabaseManager
from clawbridge.vault.vault import CredentialVault

# Initialize SQLite database schema
db = DatabaseManager(Path("clawbridge.db"))
db.initialize()

# Generate or load a 32-byte master key
master_key = EnvelopeCipher.generate_key()
cipher = EnvelopeCipher(master_key)

# Initialize the vault
vault = CredentialVault(db, cipher)
```

### 2. Store Channel Credentials

Store OAuth tokens or static credentials in the encrypted vault:

```python
# Store an OAuth2 token (e.g., Slack)
slack_token_id = vault.store_oauth_token(
    channel="slack",
    client_id="app_slack_001",
    token_data={"access_token": "xoxb-real-slack-secret-token"},
    expires_in=3600,
)

# Store static credentials (e.g., Jira basic auth or API token)
jira_cred_id = vault.store_credential(
    channel="jira",
    auth_type="basic_auth",
    credentials={"email": "bot@company.com", "token": "jira-api-token"},
)
```

### 3. Register Agents with Scoped Permissions

Create cryptographically verifiable bearer tokens for each agent with specific action rules:

```python
from clawbridge.auth.registry import AgentRegistry

registry = AgentRegistry(db)

# NemoClaw: allowed only to post to Slack
nemo_agent, nemo_bearer_token = registry.register_agent(
    name="NemoClaw",
    allowed_rules=["slack:post_message"],
)

# OpenClaw: allowed only to manage Jira
open_agent, open_bearer_token = registry.register_agent(
    name="OpenClaw",
    allowed_rules=["jira:*"],
)

print(f"NemoClaw Token: {nemo_bearer_token}")
# Output format: clw_agent_<random_hex>
```

### 4. Dispatch Outbound Requests

Set up the authenticator and egress dispatcher to make safe, authenticated requests:

```python
import asyncio
import httpx
from clawbridge.auth.authenticator import AgentAuthenticator
from clawbridge.dispatch.dispatcher import EgressDispatcher
from clawbridge.dispatch.rate_limiter import TokenBucketLimiter

auth = AgentAuthenticator(registry, vault)
limiter = TokenBucketLimiter()

async def main():
    async with httpx.AsyncClient() as client:
        dispatcher = EgressDispatcher(auth, vault, limiter, client)

        # Dispatch an authorized request
        receipt = await dispatcher.dispatch(
            bearer_header=f"Bearer {nemo_bearer_token}",
            channel="slack",
            action="post_message",
            method="POST",
            url="https://slack.com/api/chat.postMessage",
            token_id=slack_token_id,
            json={"channel": "C12345678", "text": "Task finished successfully."},
        )

        print(receipt.status)       # 'success' or 'error'
        print(receipt.status_code)  # HTTP status code
        print(receipt.data)         # Response payload

        # Attempting an unauthorized action is rejected locally
        forbidden_receipt = await dispatcher.dispatch(
            bearer_header=f"Bearer {nemo_bearer_token}",
            channel="jira",
            action="create_issue",
            method="POST",
            url="https://company.atlassian.net/rest/api/3/issue",
            credential_id=jira_cred_id,
        )

        print(forbidden_receipt.status)  # 'error'
        print(forbidden_receipt.code)    # 'FORBIDDEN'

asyncio.run(main())
```

---

## Model Context Protocol (MCP) Integration

ClawBridge exposes standardized tools to AI agents over the Model Context Protocol (FastMCP).

```python
from clawbridge.mcp.server import create_mcp_server
from clawbridge.mcp.tools import ClawBridgeTools

# Wrap dispatcher in normalized tool methods
tools = ClawBridgeTools(
    dispatcher=dispatcher,
    default_token_ids={"slack": slack_token_id},
    default_credential_ids={"jira": jira_cred_id},
)

# Create an MCPServer instance
mcp_server = create_mcp_server(tools, name="ClawBridge")
```

### Available MCP Tools

| Tool Name | Parameters | Description |
| :--- | :--- | :--- |
| `slack_post_message` | `agent_token`, `channel_id`, `text`, `token_id` | Posts a message to a Slack channel. |
| `jira_create_issue` | `agent_token`, `project_key`, `summary`, `description`, `issue_type`, `credential_id`, `jira_base_url` | Creates an issue in Atlassian Jira. |
| `twitter_post_tweet` | `agent_token`, `text`, `token_id` | Posts a tweet (X post) on behalf of an account. |
| `discord_send_message` | `agent_token`, `channel_id`, `content`, `credential_id` | Sends a message to a Discord channel. |

Each tool executes through `EgressDispatcher`, ensuring ACL verification, rate limiting, and zero credential leakage.

---

## Inbound Webhook Verification & SSE Streaming

ClawBridge protects against webhook spoofing and replay attacks, transforming incoming events into a uniform subscription stream.

```python
import asyncio
from clawbridge.inbound.verifier import WebhookVerifier
from clawbridge.inbound.normalizer import EventNormalizer
from clawbridge.inbound.broadcaster import EventBroadcaster

verifier = WebhookVerifier(db, max_clock_skew_s=300)
normalizer = EventNormalizer()
broadcaster = EventBroadcaster()

async def handle_inbound_webhook(raw_body: bytes, signature: str, timestamp: str, secret: str):
    # 1. Cryptographic HMAC and replay verification
    verified = verifier.verify_slack(
        body=raw_body,
        signature=signature,
        timestamp_header=timestamp,
        secret=secret,
    )

    # 2. Normalize platform payload to InboundEvent
    event = normalizer.normalize(verified)

    # 3. Publish to subscribed agent queues
    delivered_count = await broadcaster.publish(event)
    return delivered_count

async def agent_event_listener():
    # Agent subscribes to Slack topics
    queue = await broadcaster.subscribe(topics=["slack:*"])

    while True:
        event = await queue.get()
        print(f"Received event on topic '{event.topic}': {event.content}")
        # Format as Server-Sent Event (SSE) block
        sse_block = broadcaster.format_sse_message(event)
```

---

## Access Control Rule Reference

Permission policies follow the format:

```
<channel>:<action>
```

Wildcards (`*`) can be used for channels, actions, or prefixes:

- `slack:post_message` - Grants permission only to post messages to Slack.
- `slack:*` - Grants permission to all actions on Slack.
- `jira:read*` - Matches `read_issue`, `read_project`, etc.
- `*:read*` - Grants read operations across all channels.
- `*` - Global access across all channels and actions.

---

## Error Handling & EgressReceipt

Every egress call returns an `EgressReceipt` object:

```python
@dataclass(frozen=True)
class EgressReceipt:
    status: str             # "success" or "error"
    channel: str            # Target channel name
    status_code: int | None # Downstream or internal HTTP status code
    code: str | None        # Machine-readable error code (e.g., RATE_LIMITED)
    message: str            # Detailed explanation
    retry_after_s: float | None # Seconds to wait before retry
    retryable: bool         # Boolean flag indicating if retry is safe
    data: dict | None       # Downstream response data on success
```

### Standardized Error Codes

- `UNAUTHORIZED`: Agent bearer token missing, invalid, or expired.
- `FORBIDDEN`: Agent lacks required ACL permission for requested channel and action.
- `RATE_LIMITED`: Per-channel token bucket exhausted or downstream returned 429.
- `BAD_REQUEST`: Downstream 400 error or invalid request parameters.
- `NOT_FOUND`: Downstream resource does not exist (HTTP 404).
- `DOWNSTREAM_ERROR`: Downstream service failure (HTTP 500, 502, 503).
- `CONNECTION_ERROR`: Network timeout or DNS resolution failure.

---

## Project Structure

```
ClawBridge/
├── src/
│   └── clawbridge/
│       ├── __init__.py
│       ├── py.typed
│       ├── auth/                 # Agent authentication, tokens, and ACL
│       │   ├── acl.py            # PolicyEngine wildcard matcher
│       │   ├── authenticator.py  # AgentAuthenticator validation logic
│       │   ├── registry.py       # AgentRegistry SQLite persistence
│       │   └── tokens.py         # TokenManager (clw_agent_ generation & SHA-256)
│       ├── dispatch/             # Outbound request proxy
│       │   ├── dispatcher.py     # EgressDispatcher lifecycle coordinator
│       │   ├── errors.py         # ErrorNormalizer and EgressReceipt
│       │   ├── headers.py        # HeaderInjector for Bearer, Basic, ApiKey
│       │   └── rate_limiter.py   # TokenBucketLimiter with per-channel rates
│       ├── inbound/              # Webhook ingestion and SSE distribution
│       │   ├── broadcaster.py    # Asynchronous pub-sub EventBroadcaster
│       │   ├── normalizer.py     # EventNormalizer into canonical InboundEvent
│       │   └── verifier.py       # WebhookVerifier (HMAC + clock skew + replay cache)
│       ├── mcp/                  # Model Context Protocol server and tools
│       │   ├── server.py         # create_mcp_server factory
│       │   └── tools.py          # ClawBridgeTools synthesized tool interface
│       └── vault/                # Secure credential storage
│           ├── crypto.py         # EnvelopeCipher (AES-256-GCM)
│           ├── db.py             # DatabaseManager (SQLite in WAL mode)
│           ├── single_flight.py  # SingleFlightGroup call deduplication
│           └── vault.py          # CredentialVault storage and refresh logic
├── tests/                        # Comprehensive test suite (70 tests)
├── docs/                         # Architecture Decision Records (ADRs) and guidelines
├── pyproject.toml                # Project configuration and dependency specifications
├── LICENSE                       # MIT License
└── README.md                     # Documentation
```

---

## Testing

Run the test suite using `pytest` or `uv`:

```bash
# Run all tests
uv run pytest

# Run with verbose test output
uv run pytest -v

# Run a specific test module
uv run pytest tests/test_e2e_harness.py
```

All 70 tests cover zero-trust authentication, envelope encryption, single-flight refresh, rate limiting, header injection, webhook replay prevention, and end-to-end multi-agent tool execution.

---

## Security Best Practices

1. **Master Key Protection:** Never hardcode the AES-256 master key in source code. Supply it via an environment variable (`CLAWBRIDGE_MASTER_KEY`) or secure secret store.
2. **Database Permissions:** Restrict filesystem permissions on `clawbridge.db` and its WAL files (`chmod 600`) so only the gateway process can read the database.
3. **Agent Scope Minimization:** Apply the principle of least privilege when registering agents. Assign specific channel and action rules instead of wildcards.
4. **Clock Synchronization:** Keep system time synchronized via NTP to maintain accurate 300-second freshness checks on inbound webhooks.

---

## License

This project is licensed under the terms of the [MIT License](LICENSE).

