# 03: Outbound Egress Dispatcher & Token-Bucket Rate Limiter

**What to build:** HTTP/2 client with JIT token attachment, exponential backoff, and error normalizer

**Blocked by:** Step 2

**Status:** ready-for-agent

## Locked Architecture & Stack Constraints

- **Spec Reference:** `.preflight/SPEC.md` & `.preflight/STACK.md`
- **Memory & Agent Communication (`transactional-db (primary) + episodic-agent-state (secondary)`):** Agents communicate with ClawBridge via JSON-RPC 2.0 (Model Context Protocol stdio/SSE) and REST with Bearer tokens; ClawBridge dispatches authenticated HTTP/2 requests to platform APIs.

### Day-1 Stack (`NOW` Tools to Use)
- **FastMCP / Model Context Protocol Python SDK** (`pyproject.toml ([project] PEP 621)` — `claimed-unverified`): Exposes standardized MCP tool schemas (stdio and SSE) for autonomous agents to discover and invoke downstream actions without custom API wrappers. — _Standardizes agent integration across diverse platforms (NemoClaw, OpenClaw, Muse, Gemini) with zero custom protocol glue._
- **SQLite (WAL mode) with Cryptography (AES-256-GCM)** (`pyproject.toml ([project] PEP 621)` — `claimed-unverified`): Zero-overhead local encrypted store for per-agent credentials, platform OAuth tokens, audit logs, and inbound webhook event queues. — _Ensures zero external infrastructure dependencies for local developers while securing tokens at rest with AES-GCM envelope encryption._
- **Authlib** (`pyproject.toml ([project] PEP 621)` — `claimed-unverified`): Manages OAuth 1.0a and OAuth 2.0 token refreshes, PKCE authorization flows, and JWT validation for third-party platforms. — _Handles tricky multi-provider OAuth refresh lifecycles automatically to avoid mid-session 401 expiration failures._
- **Docker (Dockerfile)** (`Dockerfile` — `verified`): Containerizes the ClawBridge server and SQLite database for portable local daemon or cloud deployment. — _Enables one-command spun up service accessible over stdio or Docker bridge network for multi-agent clusters._

### Locked ADRs to Respect
- **ADR-0001 (Zero-Trust Agent Egress Proxy)**: Agents authenticate to ClawBridge with an Agent Bearer Token; ClawBridge retrieves platform credentials from the internal vault, attaches authentication headers, and dispatches requests. Raw platform secrets and tokens are never exposed to agents or LLM contexts.
- **ADR-0002 (Embedded Encrypted SQLite WAL Vault)**: Store platform credentials, OAuth tokens, and audit logs in a local SQLite database in WAL mode encrypted with AES-256-GCM via a master key, utilizing Authlib for automated background refresh cycles.
- **ADR-0003 (Bi-Directional Egress MCP & Inbound SSE Stream)**: Implement dual-mode communication: outbound tool calling over Model Context Protocol (stdio / SSE) and inbound webhook normalization streaming into agent-subscribed SSE topics with cryptographic HMAC signature verification.
- **ADR-0004 (Per-Channel Token-Bucket Rate Limiting & Normalized Errors)**: Implement an internal per-channel token bucket rate limiter and transform downstream API errors into structured, machine-readable JSON receipts with retry-after metadata.

### Red-Team Mitigations to Enforce
- **[CRITICAL] OAuth Token Refresh Stampede**: Implement single-flight distributed mutex per credential ID in memory so only one refresh call executes while others await the updated access token.
- **[HIGH] Inbound Webhook Replay & Spoofing**: Enforce platform HMAC-SHA256 signature verification, a strict 300-second timestamp freshness window, and SQLite-backed deduplication on webhook delivery IDs.
- **[HIGH] Agent Privilege Escalation via Unscoped Tool Dispatch**: Enforce per-agent Access Control Lists (ACLs) mapped to Agent Identity tokens, rejecting unauthorized tool calls before platform dispatch.

## Acceptance Criteria

- [ ] Deliverable verified: **HTTP/2 client with JIT token attachment, exponential backoff**.
- [ ] Deliverable verified: **error normalizer**.
- [ ] Enforces **ADR-0004 (Per-Channel Token-Bucket Rate Limiting & Normalized Errors)**: Implement an internal per-channel token bucket rate limiter and transform downstream API errors into structured, machine-readable JSON receipts with retry-after metadata.
- [ ] Enforces **ADR-0001 (Zero-Trust Agent Egress Proxy)**: Agents authenticate to ClawBridge with an Agent Bearer Token; ClawBridge retrieves platform credentials from the internal vault, attaches authentication headers, and dispatches requests. Raw platform secrets and tokens are never exposed to agents or LLM contexts.
- [ ] Integrates Day-1 tool **Authlib** (`pyproject.toml ([project] PEP 621)`) for manages oauth 1.0a and oauth 2.0 token refreshes, pkce authorization flows, and jwt validation for third-party platforms..
- [ ] Automated unit/integration tests for `outbound-egress-dispatcher-token` pass with zero regressions.
