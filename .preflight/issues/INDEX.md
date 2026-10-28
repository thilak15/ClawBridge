# Ordered Implementation Tickets: ClawBridge

Work these tickets in order (one vertical slice per session/commit). Use `Title :: Details :: BlockedBy` in `set-design --step` to declare custom or parallel dependency edges:

| # | Ticket File | Title | Blocked By | Status |
| :---: | :--- | :--- | :--- | :---: |
| `01` | [01-core-encrypted-sqlite-vault-credentials-model.md](./01-core-encrypted-sqlite-vault-credentials-model.md) | Core Encrypted SQLite Vault & Credentials Model | None (can start immediately) | `ready-for-agent` |
| `02` | [02-agent-identity-scoped-access-control-layer.md](./02-agent-identity-scoped-access-control-layer.md) | Agent Identity & Scoped Access Control Layer | Step 1 | `ready-for-agent` |
| `03` | [03-outbound-egress-dispatcher-token-bucket-rate-limiter.md](./03-outbound-egress-dispatcher-token-bucket-rate-limiter.md) | Outbound Egress Dispatcher & Token-Bucket Rate Limiter | Step 2 | `ready-for-agent` |
| `04` | [04-fastmcp-server-tool-definitions.md](./04-fastmcp-server-tool-definitions.md) | FastMCP Server & Tool Definitions | Step 3 | `ready-for-agent` |
| `05` | [05-inbound-webhook-receiver-sse-event-stream.md](./05-inbound-webhook-receiver-sse-event-stream.md) | Inbound Webhook Receiver & SSE Event Stream | Step 4 | `ready-for-agent` |
| `06` | [06-end-to-end-verification-multi-agent-test-harness.md](./06-end-to-end-verification-multi-agent-test-harness.md) | End-to-End Verification & Multi-Agent Test Harness | Step 5 | `ready-for-agent` |
