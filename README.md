# ClawBridge

[![Release](https://img.shields.io/github/v/release/thilak15/ClawBridge)](https://github.com/thilak15/ClawBridge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg)](https://go.dev)
[![Zero CGO](https://img.shields.io/badge/CGO-disabled-brightgreen.svg)](https://golang.org)

Zero-Trust Universal Connector, Credential Isolation Vault, and Model Context Protocol (MCP) Gateway for Autonomous AI Agents.

---

## Overview

ClawBridge is an ultra-lightweight, high-performance security gateway, credential isolation vault, and 24/7 background daemon written in pure Go (`CGO_ENABLED=0`).

It is designed to be the **Universal Connector & MCP Gateway** for autonomous agent ecosystems:
* **OpenClaw** ([openclaw.ai](https://openclaw.ai)) — Peter Steinberger's multi-channel personal assistant and agent runtime.
* **NVIDIA NemoClaw** ([github.com/NVIDIA/NemoClaw](https://github.com/NVIDIA/NemoClaw)) — NVIDIA's enterprise sandboxed reference stack powered by NVIDIA OpenShell and NeMo Guardrails.
* **External Agents & IDEs** — Claude Desktop, Cursor, LangChain, CrewAI, and proprietary agents connecting via Model Context Protocol (`clawbridge mcp`).

### The Problem

When autonomous agents interact with external platforms (Slack, Jira, GitHub, Discord, Twitter, Google Workspace, Meta, Telegram, WhatsApp), granting them direct access to production API keys or OAuth refresh tokens introduces severe risks:

- **Prompt Injection & Key Exfiltration:** Malicious instructions in prompt contexts or uncurated external messages can trick LLMs into exposing raw keys or session credentials.
- **Credential Fragmentation ($N \times M$ Nightmare):** Configuring keys and OAuth refresh tokens separately inside every individual agent and framework is error-prone and insecure.
- **Uncoordinated Rate Limits:** When multiple agents dispatch tasks concurrently, uncoordinated requests trigger HTTP 429 rate limits and platform bans.
- **OAuth Token Expiration:** OAuth tokens expire while agents are running 24/7 in the background, causing unexpected `401 Unauthorized` job failures.

### The ClawBridge Solution

ClawBridge completely decouples agent intelligence from credentials using a Zero-Trust architecture:

1. **Pure Go Static Binary:** Compiles with zero CGO dependencies to a single static binary (< 12MB disk, < 15MB RAM baseline, < 5ms startup). Runs on macOS, Linux, Raspberry Pi, Android/Termux, and Windows.
2. **Connect Once, Share Everywhere:** You configure your platforms (Slack, Jira, GitHub, Discord, Twitter, Telegram, WhatsApp, Meta, Google Workspace) in ClawBridge once via `clawbridge connect`. All agents share connectivity without ever seeing raw keys.
3. **Model Context Protocol (MCP) Gateway:** Exposes 22+ standardized tools over stdio JSON-RPC 2.0 (`clawbridge mcp`). Any agent speaking MCP connects out-of-the-box.
4. **First-Class Official OpenClaw & NVIDIA NemoClaw Bridge:** One-command configuration (`clawbridge bridge openclaw` and `clawbridge bridge nemoclaw`) and native runner (`clawbridge run openclaw` and `clawbridge run nemoclaw`).
5. **Embedded AES-256-GCM Vault:** Platform OAuth tokens and API secrets are stored locally in SQLite WAL mode using AES-256-GCM envelope encryption, with single-flight deduplication for automatic token refresh cycles.
6. **Token-Bucket Rate Limiter:** Enforces per-channel burst and rate controls across all running agents, preventing downstream API bans.
7. **24/7 Background Daemon:** Native OS background service management (`launchd` on macOS, `systemd` on Linux) with panic recovery, proactive 10-minute token refresher, and sleep/wake network recovery.

---

## Architecture

```
=================================================================================
                                Autonomous Agents
=================================================================================
         |                                |                             |
         v                                v                             v
+------------------+            +-------------------+         +-----------------+
|     OpenClaw     |            |  NVIDIA NemoClaw  |         | External Agents |
|  (openclaw.ai)   |            | (OpenShell & Rails|         | (Claude, Cursor,|
| ~/.openclaw/     |            |  ~/.nemoclaw/     |         |  LangChain, etc)|
+------------------+            +-------------------+         +-----------------+
         |                                |                             |
         +--------------------------------+-----------------------------+
                                          |
                              (Model Context Protocol)
                                    stdio JSON-RPC
                                          v
+-------------------------------------------------------------------------------+
|                        ClawBridge Universal Gateway                           |
|                                                                               |
|  +-------------------------+             +---------------------------------+  |
|  |     MCP Tool Server     |             |        Event Broadcaster        |  |
|  | (22+ Normalized Tools)  |             |       (Pub-Sub SSE Topics)      |  |
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
    |        |        |        |        |        |        |        |        |
    v        v        v        v        v        v        v        v        v
 [Slack]  [Jira]  [GitHub] [Discord][Twitter][Telegram][WhatsApp][ Meta ][Google]
```

---

## Universal Connectors & MCP Tools

ClawBridge exposes standardized tools via Model Context Protocol (MCP). Agents never handle credentials directly.

| Platform | Tools Exposed via MCP | Capabilities | Risk Level |
|---|---|---|---|
| **Slack** | `slack_post_message`, `slack_read_channel` | Send channel messages, read history | Standard / Low |
| **Jira** | `jira_create_issue`, `jira_search_issues` | Create tickets, JQL search queries | Standard / Low |
| **GitHub** | `github_create_issue`, `github_create_pull_request`, `github_read_repository` | Create issues, open PRs, read repos | Standard / Medium |
| **Discord** | `discord_send_message`, `discord_read_channel` | Bot messaging, inspect channel logs | Standard / Low |
| **Twitter / X** | `twitter_post_tweet`, `twitter_read_mentions` | Publish tweets, inspect user mentions | Standard / Medium |
| **Telegram** | `telegram_send_message`, `telegram_get_updates` | Send bot messages, fetch updates | Standard / Low |
| **WhatsApp** | `whatsapp_send_message`, `whatsapp_send_template` | Cloud API text and template messages | Standard / Medium |
| **Meta Graph** | `instagram_post_publish`, `instagram_read_comments`, `instagram_reply_comment`, `facebook_post_feed`, `facebook_read_page_feed` | Instagram media containers, comments, Facebook page feeds | High (gated) |
| **Google Workspace** | `calendar_list_events`, `calendar_find_free_slots`, `calendar_create_event`, `gmail_search_messages`, `gmail_read_thread`, `gmail_create_draft`, `gmail_send_message` | Calendar scheduling, busy/free slots, Gmail search, drafting, sending | High (gated) |

---

## Quickstart (30 Seconds)

### 1. Download or Build Static Binary

#### Option A: One-Liner Install
```bash
curl -sSL https://raw.githubusercontent.com/thilak15/ClawBridge/main/scripts/install.sh | bash
```

Ensure `~/.local/bin` is in your `$PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

#### Option B: Direct Precompiled Binary Download
Download static binaries directly from [GitHub Releases (v1.0.1)](https://github.com/thilak15/ClawBridge/releases/latest):
* `clawbridge-darwin-arm64` (macOS Apple Silicon)
* `clawbridge-darwin-amd64` (macOS Intel)
* `clawbridge-linux-amd64` (Linux x86_64)
* `clawbridge-linux-arm64` (Linux ARM64 / Raspberry Pi)
* `clawbridge-windows-amd64.exe` (Windows x64)

#### Option C: Build from Source (Pure Go, Zero CGO)
```bash
git clone https://github.com/thilak15/ClawBridge.git
cd ClawBridge
make build
./bin/clawbridge --version
```

---

## Official Claws Integration (OpenClaw & NVIDIA NemoClaw)

### Running Official OpenClaw (openclaw.ai)

ClawBridge automatically configures OpenClaw's MCP server and launches the runtime:

```bash
# 1. Configure OpenClaw MCP integration (~/.openclaw/openclaw.json)
clawbridge bridge openclaw

# 2. Launch official OpenClaw runtime with ClawBridge MCP tools
clawbridge run openclaw
```

If OpenClaw is not yet installed on your system, ClawBridge will guide you to install it:
```bash
npm install -g openclaw
# or
curl -fsSL https://openclaw.ai/install.sh | bash
```

### Running Official NVIDIA NemoClaw (NVIDIA/NemoClaw)

ClawBridge generates NVIDIA OpenShell sandbox policies and NeMo Guardrail configurations:

```bash
# 1. Configure NVIDIA OpenShell sandbox policy (~/.nemoclaw/sandbox.yaml)
clawbridge bridge nemoclaw

# 2. Launch official NemoClaw / OpenShell sandbox
clawbridge run nemoclaw
```

---

## Interactive Command Reference

### Universal Connector & Gateway Console
```bash
clawbridge
```

Inside the console:
```text
clawbridge> help
Available Commands:
  Universal Connector & MCP Tools:
    tools                    - List all active Model Context Protocol (MCP) tools
    vault                    - Inspect encrypted credentials and token refresh state
    connect [app]            - Connect platforms (Slack, Jira, GitHub, Google, Meta, etc.)

  Official Upstream Claws Bridge:
    bridge openclaw          - Configure OpenClaw MCP integration (~/.openclaw/openclaw.json)
    bridge nemoclaw          - Configure NVIDIA NemoClaw sandbox (~/.nemoclaw/sandbox.yaml)
    run openclaw [args...]   - Launch official OpenClaw with ClawBridge MCP tools
    run nemoclaw [args...]   - Launch official NVIDIA NemoClaw / OpenShell stack

  Console Controls:
    clear                    - Clear terminal console screen
    help                     - Show this guidance message
    exit / quit              - Leave the console
```

### Connector Setup Wizard
Connect platforms step-by-step with automatic AES-256-GCM encryption into the local vault:
```bash
# Interactive selection menu
clawbridge connect

# Direct platform connectors
clawbridge connect slack
clawbridge connect jira
clawbridge connect github
clawbridge connect discord
clawbridge connect twitter
clawbridge connect telegram
clawbridge connect whatsapp
clawbridge connect google
clawbridge connect meta
```

### Agent Configuration & Editing
Agent YAML files are located at:
* Inside repository: `agents/<name>.yaml`
* Global installation: `~/.clawbridge/agents/<name>.yaml`

Manage them easily with CLI commands:
```bash
# Create a new agent configuration
clawbridge agent create researcher

# List all configured agents
clawbridge agent list

# Open and edit agent YAML directly in your $EDITOR
clawbridge agent edit researcher
```

### Model Context Protocol (MCP) Stdio Server
Expose ClawBridge as a standard MCP server for Claude Desktop, Cursor, or any external framework:
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

## 24/7 Background Daemon

ClawBridge runs 24/7 in the background to proactively refresh OAuth tokens, enforce cross-agent rate limits, and verify incoming webhooks.

```bash
# Install and start background OS service (launchd on macOS, systemd on Linux)
clawbridge service install
clawbridge service start

# Inspect status and tail logs
clawbridge service status
clawbridge service logs

# Stop and uninstall service
clawbridge service stop
clawbridge service uninstall
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
