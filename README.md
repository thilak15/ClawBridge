# ClawBridge

[![Release](https://img.shields.io/github/v/release/thilak15/ClawBridge)](https://github.com/thilak15/ClawBridge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg)](https://go.dev)
[![Zero CGO](https://img.shields.io/badge/CGO-disabled-brightgreen.svg)](https://golang.org)

Zero-Trust Agent Egress Gateway, Credential Vault, and 24/7 Multi-Agent Daemon for Autonomous AI.

---

## Overview

ClawBridge is an ultra-lightweight, high-performance security gateway, credential isolation vault, and 24/7 multi-agent daemon written in pure Go (`CGO_ENABLED=0`).

It is designed for autonomous AI systems (including NemoClaw, OpenClaw, Gemini, and proprietary agents such as Muse or Claude Desktop) that require continuous, long-running connectivity to external platforms (Slack, Jira, GitHub, Discord, Twitter/X) without exposing production credentials or exhausting system memory.

### The Problem

When autonomous agents interact with external platforms, granting them direct access to production API keys or OAuth refresh tokens introduces severe risks:

- **Prompt Injection and Exfiltration:** Malicious instructions in prompt contexts or uncurated external inputs can trick LLMs into exposing raw keys or session credentials.
- **Context Leaks:** API keys, access tokens, and sensitive headers easily leak into conversation history, token logs, telemetry, and debugging artifacts.
- **Uncontrolled Blast Radius:** Direct access lacks centralized rate limiting, per-agent action whitelisting, or audit trails, risking cascading API bans and unintentional service outages.
- **Resource Bloat in 24/7 Daemons:** Traditional multi-repo Python agents consume hundreds of megabytes of RAM and leak memory over time as conversation histories grow unbounded.

### The ClawBridge Solution

ClawBridge decouples agent intelligence from credentials using a Zero-Trust architecture:

1. **Pure Go Static Binary:** Compiles with zero CGO dependencies to a single static binary (< 12MB disk, < 15MB RAM baseline, < 5ms startup). Runs on macOS, Linux, Raspberry Pi, Android/Termux, and Windows.
2. **Single-Repository Model:** Native open-source agent runtimes (`nemoclaw`, `openclaw`) live directly in `agents/` in this repository. External proprietary agents (Muse, Claude Desktop, Cursor) connect via Model Context Protocol (`clawbridge mcp`).
3. **Universal Access with Granular Tuning:** Agents have universal access by default (`*`), or administrators can customize per-agent app permissions via the interactive wizard (`clawbridge config`).
4. **Embedded AES-256-GCM Vault:** Platform OAuth tokens and API secrets are stored locally in SQLite WAL mode using AES-256-GCM envelope encryption, with single-flight deduplication for automatic token refresh cycles.
5. **Auto-Pruned Sliding Context Window:** Retains exactly the last 15 conversation turns per agent in memory, permanently capping RAM usage under 15MB regardless of how long agents run.
6. **24/7 Daemon Architecture:** Native OS background service management (`launchd` on macOS, `systemd` on Linux), supervisor watchdog with 2-second panic auto-restart, proactive 10-minute token refresher, sleep/wake network jump recovery, and 10MB x 3 file log rotation.

### How Multiple Agents Share Connectors (Universal Connectivity)

- **One Setup for All Apps:** You configure your Slack bot token, Jira credentials, Twitter API keys, Discord bot token, and GitHub personal access token in ClawBridge once. You never have to configure those apps inside individual agents.
- **Universal Multi-Tasking:** Every agent is general-purpose and can connect to any integrated application to perform any task, individually or simultaneously.
- **Coordinated Rate Limiting Across Agents:** If NemoClaw and OpenClaw both try to send messages to Slack simultaneously, ClawBridge queues and paces them so they share the platform quota without triggering downstream rate-limit bans.
- **Shared Background Token Refreshes:** When an OAuth token expires, ClawBridge refreshes it once behind the scenes using single-flight deduplication. Neither agent experiences interruption; their requests continue to work uninterrupted.
- **Shared Inbound Event Switchboard:** When an inbound webhook arrives from Slack, Jira, or GitHub, ClawBridge cryptographically verifies HMAC signatures and routes the event directly to subscribed agents.

---

## Architecture

```
=================================================================================
                                  ClawBridge CLI
             (Single Repository: Clone Once, Run Everything 24/7)
=================================================================================
                                        |
     +----------------------------------+----------------------------------+
     |                                  |                                  |
     v                                  v                                  v
+-----------------------+  +-----------------------+  +-----------------------+
|       NemoClaw        |  |       OpenClaw        |  |    External Agents    |
|   (Built-in Agent)    |  |   (Built-in Agent)    |  |  (Muse, Claude, etc.) |
|  General Purpose ACL  |  |  Engineering ACL      |  |  Via Model Context    |
|  Sliding Window (15)  |  |  Sliding Window (15)  |  |  Protocol (MCP Stdio) |
+-----------------------+  +-----------------------+  +-----------------------+
     |                                  |                                  |
     +----------------------------------+----------------------------------+
                                        |
                    (clw_agent_* Tokens & Scoped Policies)
                                        v
+-------------------------------------------------------------------------------+
|                                ClawBridge Core                                |
|                                                                               |
|  +-------------------------+             +---------------------------------+  |
|  |   Agent Authenticator   |             |        Event Broadcaster        |  |
|  |     & Policy Engine     |             |       (Pub-Sub SSE Topics)      |  |
|  +-------------------------+             +---------------------------------+  |
|               |                                            ^                  |
|  +-------------------------+             +---------------------------------+  |
|  |   Token Bucket Limiter  |             |         Event Normalizer        |  |
|  | (Per-Channel Rate/Burst)|             |   (Slack, Jira, Generic Schema) |  |
|  +-------------------------+             +---------------------------------+  |
|               |                                            ^                  |
|  +-------------------------+             +---------------------------------+  |
|  |    Egress Dispatcher    |             |         Webhook Verifier        |  |
|  | (JIT Header Injection)  |             |    (HMAC-SHA256, Replay Cache)  |  |
|  +-------------------------+             +---------------------------------+  |
|               |                                            ^                  |
|  +-------------------------------------------------------+ |                  |
|  |               Encrypted Vault (SQLite WAL)            | |                  |
|  |      AES-256-GCM Master Key + SingleFlight Refresh    | |                  |
|  +-------------------------------------------------------+ |                  |
|               |                                            ^                  |
|  +-------------------------------------------------------------------------+  |
|  |                      24/7 Daemon Supervisor & Watchdog                  |  |
|  |      Panic Recovery (2s) | Proactive Refresher (10m) | Sleep/Wake Detection|
|  +-------------------------------------------------------------------------+  |
+-------------------------------------------------------------------------------+
         |                     |                     |                     |
         v                     v                     v                     v
     [ Slack ]             [ Jira ]             [ Twitter ]           [ Discord ]
```

---

## Key Features

- **Zero-Trust Credential Isolation:** Raw API keys, basic auth secrets, and OAuth refresh tokens never reach agent contexts or LLM prompts.
- **Embedded AES-256-GCM Vault:** Backed by SQLite in WAL (Write-Ahead Logging) mode. All credentials and OAuth secrets are encrypted at rest with high-entropy nonces.
- **Single-Flight OAuth Refresh:** When multiple concurrent agent requests encounter an expired token, the refresh operation is deduplicated so the downstream OAuth provider is called exactly once.
- **Fine-Grained Agent Identity and ACLs:** Define exact permission scopes per agent (e.g., `slack:post_message`, `jira:read*`, `*`). Requests violating policy are rejected immediately.
- **Per-Channel Token Bucket Rate Limiting:** Enforces configurable burst capacities and replenishment rates per platform. Automatically applies backoff windows when downstream platforms return HTTP 429.
- **Normalized Execution Receipts:** Outbound actions return a unified `EgressReceipt` with status codes, machine-readable error codes (`UNAUTHORIZED`, `RATE_LIMITED`, `DOWNSTREAM_ERROR`), and `retry_after_s` metadata.
- **Model Context Protocol (MCP) Integration:** Exposes normalized tools (`slack_post_message`, `jira_create_issue`, `twitter_post_tweet`, `discord_send_message`) directly through stdio JSON-RPC 2.0.
- **Cryptographic Webhook Verification:** Verifies inbound signatures (Slack `v0=...`, GitHub / generic `sha256=...`), enforces a 300-second freshness window against clock skew, and deduplicates delivery IDs in SQLite to block replay attacks.
- **Asynchronous Inbound SSE Broadcaster:** Dispatches normalized inbound events (`InboundEvent`) to listening agents matching topic subscriptions and wildcards.
- **Supervisor Watchdog:** Catches panics in agent worker goroutines with automatic 2-second recovery.
- **Proactive Token Refresher:** Background ticker inspecting OAuth tokens every 10 minutes and refreshing any token within 5 minutes of expiration.
- **Sleep/Wake Network Recovery:** Detects system sleep/wake clock jumps and re-establishes connectivity automatically.
- **Capped Log Rotation:** Rotates at 10MB with maximum 3 backup files, keeping total log and database footprint permanently under 35MB.

---

## Quickstart (30 Seconds)

### 1. Download or Build Static Binary

#### Option A: One-Liner Install

```bash
curl -sSL https://raw.githubusercontent.com/thilak15/ClawBridge/main/scripts/install.sh | bash
```

Ensure `~/.local/bin` is in your `$PATH` if it is not already:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

#### Option B: Direct Precompiled Binary Download

Download static binaries directly from [GitHub Releases (v1.0.1)](https://github.com/thilak15/ClawBridge/releases/latest) for your target platform:
- `clawbridge-darwin-arm64` (macOS Apple Silicon)
- `clawbridge-darwin-amd64` (macOS Intel)
- `clawbridge-linux-amd64` (Linux x86_64)
- `clawbridge-linux-arm64` (Linux ARM64 / Raspberry Pi)
- `clawbridge-windows-amd64.exe` (Windows x64)

#### Option C: Build from Source (Pure Go, Zero CGO)

```bash
git clone https://github.com/thilak15/ClawBridge.git
cd ClawBridge
make build
./bin/clawbridge --version
```

### 2. First-Run Guided Setup

Simply run the binary without arguments:

```bash
clawbridge
```

ClawBridge will automatically:
1. Detect or prompt you to select your LLM Provider (Google Gemini, OpenAI, Anthropic Claude, or local Ollama). Supports both Google AI Studio auth keys (`AQ...`) and standard keys (`AIza...`).
2. Offer to connect your first app integration (Slack, Jira, Discord, Twitter, or GitHub).
3. Initialize built-in agents (`nemoclaw` and `openclaw`).
4. Launch the interactive multi-agent console.

---

## Command Reference

### Interactive Multi-Agent Runner

```bash
# Run all configured agents concurrently
clawbridge run --all

# Run specific agents
clawbridge run nemoclaw openclaw
```

Inside the interactive console:
```text
clawbridge> help
Available Commands:
  Agent Instructions:
    <agent_name>: <task>     - Send a direct instruction to an agent
    broadcast: <task>        - Send an instruction to all running agents

  Management & Configuration:
    connect [app]            - Connect platforms (Slack, Jira, etc.) or LLMs
    config                   - Configure per-agent access permissions
    agent create <name>      - Create and start a new autonomous agent dynamically
    run <agent>              - Start an inactive agent
    list                     - List all running agents
    status                   - Show agent turn counts and active memory
    clear                    - Clear terminal console screen
    help                     - Show this guidance message
    exit / quit              - Leave the console

clawbridge> nemoclaw: Summarize unread messages in #general
clawbridge> openclaw: Check open sprint issues in Jira
clawbridge> broadcast: Prepare weekly synchronization report
clawbridge> connect gemini
clawbridge> agent create researcher
clawbridge> config
```

### Connector Wizard

Connect platforms and LLM providers step-by-step with automatic encryption into the local vault:

```bash
# Interactive selection menu (Apps & LLM providers)
clawbridge connect

# Direct application connectors
clawbridge connect slack
clawbridge connect jira
clawbridge connect discord
clawbridge connect twitter
clawbridge connect github

# Direct LLM provider connectors
clawbridge connect gemini     # Supports AQ... and AIza... keys
clawbridge connect openai     # Supports sk-... keys
clawbridge connect anthropic  # Supports sk-ant-... keys
```

### Permission Wizard

Inspect and adjust app permissions per agent:

```bash
clawbridge config
```

Lets you select an agent and choose:
1. Full Access to All Apps (`*`)
2. Granular Selection (toggle Slack, Jira, Twitter, Discord, GitHub individually)

### Dynamic Agent Creation

Create a new custom autonomous agent:

```bash
clawbridge agent create researcher
```

Generates a configuration file in `agents/researcher.yaml` with universal permissions and registers the agent in the SQLite vault.

### Model Context Protocol (MCP) Server

Connect proprietary or external agents (e.g. Muse, Claude Desktop, Cursor) to ClawBridge:

```bash
clawbridge mcp
```

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "clawbridge": {
      "command": "clawbridge",
      "args": ["mcp"]
    }
  }
}
```

---

## 24/7 Daemon Operation

ClawBridge provides production-grade background daemon management for running agents continuously.

### Native OS Background Service

#### Install and Start Service

On macOS (via `launchd`):
```bash
clawbridge service install
clawbridge service start
```

On Linux (via `systemd`):
```bash
clawbridge service install
clawbridge service start
```

#### Check Service Status and Logs

```bash
# Inspect status
clawbridge service status

# Tail background logs
clawbridge service logs
```

#### Stop and Uninstall Service

```bash
clawbridge service stop
clawbridge service uninstall
```

### Detached Background Daemon

If you prefer running without OS service managers:

```bash
# Start detached daemon in background (tracks PID in ~/.clawbridge/daemon.pid)
clawbridge start -d

# Check status
clawbridge service status
```

---

## Cross-Platform Compilation

ClawBridge compiles 100% statically without CGO for all major operating systems and CPU architectures:

```bash
make build-all
```

Produces:
- `dist/clawbridge-darwin-arm64` (Apple Silicon)
- `dist/clawbridge-darwin-amd64` (macOS Intel)
- `dist/clawbridge-linux-amd64` (Linux x86_64)
- `dist/clawbridge-linux-arm64` (Linux ARM64 / Raspberry Pi / Android Termux)
- `dist/clawbridge-windows-amd64.exe` (Windows x64)

Each artifact includes a matching `.sha256` verification checksum.

---

## Configuration and Environment

ClawBridge automatically detects and encrypts secrets from a local `.env` file into the SQLite vault on startup:

```bash
# LLM Providers (at least one recommended)
GEMINI_API_KEY="AQ.Ab8... or AIza..."
OPENAI_API_KEY="sk-..."
ANTHROPIC_API_KEY="sk-ant-..."
OLLAMA_HOST="http://localhost:11434"

# Platform Credentials (optional, or use 'clawbridge connect')
SLACK_BOT_TOKEN="xoxb-..."
SLACK_SIGNING_SECRET="..."

JIRA_BASE_URL="https://your-domain.atlassian.net"
JIRA_EMAIL="developer@company.com"
JIRA_API_TOKEN="..."

DISCORD_BOT_TOKEN="..."
TWITTER_BEARER_TOKEN="..."
GITHUB_TOKEN="ghp_..."
```

---

## Testing

Run the full automated test suite:

```bash
# Run all Go unit, integration, and E2E tests
make test
```

---

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
