# ADR-0001: Zero-Trust Agent Egress Proxy

- **Status:** Accepted
- **Date:** 2026-10-28
- **Dimension:** `scope`

## Decision

Agents authenticate to ClawBridge with an Agent Bearer Token; ClawBridge retrieves platform credentials from the internal vault, attaches authentication headers, and dispatches requests. Raw platform secrets and tokens are never exposed to agents or LLM contexts.

## Context & Rationale

Direct token distribution leaves credentials vulnerable to prompt injection exfiltration, context log leaking, and decentralized token management.

## Considered & Rejected Alternatives

Direct secret distribution to agents (where agents fetch tokens from ClawBridge and make direct API calls to Slack/Jira/X).
