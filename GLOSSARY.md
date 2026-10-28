# ClawBridge Domain Glossary

## Core Terms

- **Agent Gateway**: The centralized egress broker and protocol adapter that exposes uniform interfaces (MCP / REST) to AI agents and dispatches requests to downstream APIs.
  _Avoid_: proxy, shim, middleware wrapper

- **Credential Vault**: An encrypted zero-trust store holding third-party OAuth refresh tokens, API keys, and session cookies, enforcing agent-level access control lists (ACLs) and automated token refreshing without leaking raw secrets to calling agents.
  _Avoid_: secret manager, key repository

- **Egress Channel**: A downstream third-party platform API, categorized into Social (X/Twitter, LinkedIn, Telegram, Discord) and Enterprise Productivity (Slack, Jira, GitHub, Notion).
  _Avoid_: integration, plugin, external tool

- **Agent Identity**: A unique, cryptographically verifiable credential (e.g. scoped bearer token or mTLS certificate) identifying the calling AI agent (e.g. NemoClaw, OpenClaw, Muse) to enforce rate limits, action whitelists, and audit trails.
  _Avoid_: user account, client id

- **Synthesized Tool Interface**: The normalized MCP (Model Context Protocol) or JSON-RPC tool schema exposed by ClawBridge to agents, abstracting away channel-specific REST/GraphQL quirks.
  _Avoid_: wrapper function, custom endpoint

