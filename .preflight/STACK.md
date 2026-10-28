# Stack Explained & Memory Architecture: ClawBridge

## 1. Stack Explained (Tooling, Alternatives, Now vs. Later & Proof Status)

| Tool | Stage (`Now` / `Later`) | What Job It Does | Why Needed | Alternative Considered | Config Proof & Verification |
| :--- | :---: | :--- | :--- | :--- | :--- |
| **FastMCP / Model Context Protocol Python SDK** (`core-oss`) | **NOW** | Exposes standardized MCP tool schemas (stdio and SSE) for autonomous agents to discover and invoke downstream actions without custom API wrappers. | Standardizes agent integration across diverse platforms (NemoClaw, OpenClaw, Muse, Gemini) with zero custom protocol glue. | Custom REST/GraphQL APIs (rejected because every agent would require custom client tool definitions rather than native MCP compatibility). | ⚠️ Claimed / Unverified: `pyproject.toml ([project] PEP 621)` |
| **SQLite (WAL mode) with Cryptography (AES-256-GCM)** (`storage-memory`) | **NOW** | Zero-overhead local encrypted store for per-agent credentials, platform OAuth tokens, audit logs, and inbound webhook event queues. | Ensures zero external infrastructure dependencies for local developers while securing tokens at rest with AES-GCM envelope encryption. | HashiCorp Vault or AWS Secrets Manager (rejected for MVP due to external operational overhead and cloud lock-in). | ⚠️ Claimed / Unverified: `pyproject.toml ([project] PEP 621)` |
| **Authlib** (`core-oss`) | **NOW** | Manages OAuth 1.0a and OAuth 2.0 token refreshes, PKCE authorization flows, and JWT validation for third-party platforms. | Handles tricky multi-provider OAuth refresh lifecycles automatically to avoid mid-session 401 expiration failures. | Custom OAuth refresh loops (rejected as error-prone across differing provider specs like Twitter vs Slack vs Jira). | ⚠️ Claimed / Unverified: `pyproject.toml ([project] PEP 621)` |
| **Docker (Dockerfile)** (`container-runtime`) | **NOW** | Containerizes the ClawBridge server and SQLite database for portable local daemon or cloud deployment. | Enables one-command spun up service accessible over stdio or Docker bridge network for multi-agent clusters. | Bare-metal process running on host OS (rejected due to Python environment conflicts and permission isolation). | ✅ Verified (`scout:Dockerfile`): `Dockerfile` |

- **Verification Summary:** 1 Verified (backed by `scout.py` or workspace files) | 3 Claimed / Unverified (recommended for target architecture, not yet present on disk).

## 2. Memory, State & Agent Communication Architecture

- **Primary Memory Strategy:** `transactional-db`
- **Secondary / Combined Strategies:** `episodic-agent-state`
- **Combined Memory Profile:** `transactional-db (primary) + episodic-agent-state (secondary)`
- **Agent / Component Communication Pattern:** Agents communicate with ClawBridge via JSON-RPC 2.0 (Model Context Protocol stdio/SSE) and REST with Bearer tokens; ClawBridge dispatches authenticated HTTP/2 requests to platform APIs.
- **Workflow Memory Lifecycle:** Credentials, OAuth tokens, and audit receipts are stored in an encrypted SQLite database using WAL mode; tokens are decrypted just-in-time for downstream dispatch; inbound events append to an indexed event log and push to connected agent SSE event queues.
- **Why This Strategy (vs. RAG / Vector DB / Alternatives):** Transactional SQLite with WAL mode provides embedded zero-overhead ACID transactions, robust single-writer concurrent reads, and zero external infrastructure dependencies, superior to complex distributed databases for local/containerized agent bridges.
- **Alternatives Considered:** External HashiCorp Vault (rejected for MVP due to heavy operational setup overhead); Raw environment variables or plain JSON config files (rejected due to severe credential leakage risks and inability to support live automated token refresh).

### Anticipated Memory Failure Modes & Mitigations

- ⚠️ Database locking contention (SQLITE_BUSY) under concurrent agent bursts, mitigated by enabling WAL mode, a 5000ms busy timeout, and a thread-safe connection pool.
- ⚠️ Token refresh race conditions where multiple parallel agent requests trigger simultaneous OAuth token refresh calls, mitigated by single-flight mutex locking per channel credentials.

