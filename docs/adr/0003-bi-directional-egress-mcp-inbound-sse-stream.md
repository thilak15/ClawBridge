# ADR-0003: Bi-Directional Egress MCP & Inbound SSE Stream

- **Status:** Accepted
- **Date:** 2026-10-28
- **Dimension:** `scope`

## Decision

Implement dual-mode communication: outbound tool calling over Model Context Protocol (stdio / SSE) and inbound webhook normalization streaming into agent-subscribed SSE topics with cryptographic HMAC signature verification.

## Context & Rationale

Modern autonomous agents (NemoClaw, Muse, OpenClaw) require event-driven reactive wakeups (e.g. Slack mentions, Jira issue updates) alongside proactive tool execution.

## Considered & Rejected Alternatives

Outbound-only gateway or raw unverified webhook proxies.
