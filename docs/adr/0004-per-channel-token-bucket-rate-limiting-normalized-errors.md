# ADR-0004: Per-Channel Token-Bucket Rate Limiting & Normalized Errors

- **Status:** Accepted
- **Date:** 2026-10-28
- **Dimension:** `bottlenecks`

## Decision

Implement an internal per-channel token bucket rate limiter and transform downstream API errors into structured, machine-readable JSON receipts with retry-after metadata.

## Context & Rationale

Prevents agent execution crashes (addressing n8n-io/n8n#24042) and avoids triggering severe downstream API bans.

## Considered & Rejected Alternatives

Pass-through raw HTTP errors or unhandled socket exceptions.
