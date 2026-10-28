# ADR-0002: Embedded Encrypted SQLite WAL Vault

- **Status:** Accepted
- **Date:** 2026-10-28
- **Dimension:** `memory`

## Decision

Store platform credentials, OAuth tokens, and audit logs in a local SQLite database in WAL mode encrypted with AES-256-GCM via a master key, utilizing Authlib for automated background refresh cycles.

## Context & Rationale

Zero external infrastructure dependencies for developers while preventing token leakage and enabling seamless background OAuth rotation.

## Considered & Rejected Alternatives

External secret management daemons (HashiCorp Vault, AWS Secrets Manager) or plaintext environment variables.
