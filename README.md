# 🐱 MeowClaw — Lightweight AI Gateway

<p align="center">
  <img src="assets/meowclaw-logo.png" alt="MeowClaw" width="600">
</p>

<p align="center">
  <strong>A fast, stable, single-binary AI assistant gateway built with Go.</strong>
</p>

<p align="center">
  <em>"What OpenClaw does, in 1/10 the binary size, 10x more stable."</em><br>
  The lobster is big and heavy. The cat is light and sharp. 🐱
</p>

## Why MeowClaw?

MeowClaw targets the same job as heavy “personal AI gateway” stacks — **many chat channels → one LLM → replies back** — but optimizes for **small deploys, predictable ops, and long uptimes**. You get a **single static binary**, **declarative YAML**, and **no Node runtime or container mandatory path**.

| | OpenClaw (Node.js) | MeowClaw (Go) |
|---|---|---|
| Install | `npm install -g` + Node 24 | **Single binary**, no runtime install |
| Binary size | ~200MB+ (`node_modules`) | **~20MB** (stripped production build) |
| Memory | Often 1GB+ in the wild | **Tens of MB** typical |
| Stability | Frequent gateway restarts / OOM reports | **Long-lived process**; channel work isolated in goroutines |
| Setup | Large JSON surface | **`meowclaw init`** + `~/.meowclaw/config.yaml` |
| WhatsApp | Baileys (JS) — common disconnect pain | **[whatsmeow](https://github.com/tulir/whatsmeow)** (Go), keepalive + WAL credentials |
| Slack | Varies by setup | **Socket Mode** — no public inbound URL |
| LLM | Often OpenAI-centric | **OpenAI + Anthropic**, streaming, per-session `/model` |
| Secrets | Often only on disk | **Env overrides** + optional **gateway `api_token`**, config `0600` |
| Ops profile | Daemon + package ecosystem | **Copy binary, systemd/launchd, edge device** |

**Good fit if you want:** one repo to read, minimal moving parts, SSH-to-VPS or homelab install, CI-friendly builds, and channels without shipping a full JS platform.

**Different direction than:** “clone a large TypeScript monorepo + Docker + vendor CLI to operate” — MeowClaw is deliberately **boring infra**: compile, configure, run.

## Quick Start

```bash
# Install
go install github.com/YoungsoonLee/meowclaw/cmd/meowclaw@latest

# Interactive setup
meowclaw init

# Start
meowclaw up
```

## Features

- **Multi-channel**: Telegram, Discord, WhatsApp (whatsmeow), Slack (Socket Mode), WebChat
- **Multi-provider AI**: OpenAI, Anthropic (extensible; optional `base_url` + API path overrides)
- **Persistent memory**: SQLite + FTS5 full-text search
- **Streaming responses**: ChatGPT-like real-time token delivery via WebSocket
- **WebSocket API**: Real-time message streaming
- **Web dashboard**: Built-in status & chat UI
- **Single binary**: No runtime dependencies
- **Security-minded defaults**: loopback bind, optional gateway API token, WebSocket Origin allowlist, config file `0600`, env overrides for secrets
- **Chat commands**: `/new`, `/reset`, `/status`, `/model` (per-session model override) on any channel
- **Channel auto-reconnect**: external bridges (Telegram, Discord, Slack, WhatsApp) retry with exponential backoff (1s → … → 5m cap); WebChat is in-process only

## Architecture

```
Telegram / Discord / WhatsApp / Slack / WebChat
               |
               v
   +------------------------+
   |   MeowClaw Gateway     |
   |  ws://127.0.0.1:6820  |
   +----------+-------------+
              |
    +---------+---------+
    |                   |
  Agent            WebSocket
  (LLM)           Clients
    |
  Memory
  (SQLite)
```

## Configuration

Config lives at `~/.meowclaw/config.yaml`. See `config.example.yaml` for all options.

```yaml
gateway:
  port: 6820
  host: "127.0.0.1"

channels:
  telegram:
    enabled: true
    bot_token: "YOUR_TOKEN"

agent:
  provider: openai   # openai | anthropic
  openai:
    api_key: "sk-..."
    model: "gpt-4o"
  anthropic:
    api_key: ""
    model: "claude-sonnet-4-20250514"

memory:
  enabled: true
  db_path: "~/.meowclaw/memory.db"
```

### LLM API base URL and paths

You can point OpenAI- and Anthropic-compatible APIs at a **proxy**, **regional endpoint**, or a **new API path** without recompiling.

| Key | Provider | Purpose | If omitted |
|-----|----------|---------|------------|
| `base_url` | `openai` | Host only (e.g. `https://api.openai.com`) | Official OpenAI host |
| `chat_path` | `openai` | Path segment (e.g. `/v1/chat/completions`) | `/v1/chat/completions` |
| `base_url` | `anthropic` | Host only (e.g. `https://api.anthropic.com`) | Official Anthropic host |
| `messages_path` | `anthropic` | Path segment (e.g. `/v1/messages`) | `/v1/messages` |

Paths may be written with or without a leading `/`. The final request URL is `base_url` + path.

OpenAI (optional overrides):

```yaml
agent:
  provider: openai
  openai:
    api_key: "sk-..."
    model: "gpt-4o"
    base_url: "https://api.openai.com"
    chat_path: "/v1/chat/completions"
```

Anthropic (optional overrides):

```yaml
agent:
  provider: anthropic
  anthropic:
    api_key: "sk-ant-..."
    model: "claude-sonnet-4-20250514"
    base_url: "https://api.anthropic.com"
    messages_path: "/v1/messages"
```

## Security

MeowClaw is designed to avoid the class of issues seen in large “always-on” AI gateways: **internet-exposed control planes**, **unauthenticated HTTP/WebSocket**, and **secrets-only-on-disk**.

| Topic | What we do |
|-------|------------|
| **Network** | Default `gateway.host` is `127.0.0.1`. Binding to `0.0.0.0` or a LAN IP logs a warning; use a reverse proxy + TLS for remote access, not raw exposure. |
| **Gateway token** | Optional `gateway.api_token`. When set, `POST /api/send` and `GET /api/channels` require `Authorization: Bearer <token>`. WebSocket accepts the same token as `?token=` (dashboard: open `http://127.0.0.1:6820/?token=YOUR_TOKEN`). |
| **WebSocket Origin** | By default, only Origins matching your gateway host/port and `localhost` are allowed. Override with `gateway.trusted_origins` or, only if you must, `gateway.allow_any_websocket_origin: true`. |
| **Secrets** | `ApplySecretsFromEnv` after load: `MEOWCLAW_OPENAI_API_KEY`, `MEOWCLAW_ANTHROPIC_API_KEY`, `MEOWCLAW_GATEWAY_API_TOKEN` override YAML (so production can avoid keys in files). `meowclaw send` accepts `--api-token` or `MEOWCLAW_GATEWAY_API_TOKEN`. |
| **Config file** | Saved with mode `0600`; config directory created as `0700`. |
| **Input limits** | `gateway.max_api_body_bytes` (default 1 MiB) for `/api/send`; `agent.max_input_runes` (default 100000) for one user message (HTTP, WebSocket WebChat, and agent). |
| **Data at rest** | Conversation memory is SQLite under `~/.meowclaw/` (not encrypted in-app). Protect the directory (permissions, full-disk encryption); treat `config.yaml` as sensitive. |
| **Command injection** | No shell execution of user or config strings in the gateway path; `meowclaw send` uses JSON encoding (not string formatting) for the request body. |

`GET /api/health` stays unauthenticated for simple liveness checks; it does not return API keys.

### Chat commands (Telegram, Discord, Slack, WebChat, …)

Send these as a normal message (leading `/`). They are handled locally and **do not** call the LLM or store the command text in session history.

| Command | Behavior |
|---------|----------|
| `/new` | Clear this chat’s session history and per-chat model override. |
| `/reset` | Same as `/new` (alias). |
| `/status` | Show provider, config default model, effective model for this chat, and session id. |
| `/model` | Show current model for this chat and usage hint. |
| `/model <name>` | Use `<name>` for **this chat only** in API requests (e.g. `gpt-4o-mini`). `/new` clears the override. |

## CLI Commands

```bash
meowclaw init          # Interactive setup wizard
meowclaw up            # Start the gateway
meowclaw up -v         # Start with verbose logging
meowclaw status        # Check gateway health
meowclaw send --channel telegram --to CHAT_ID -m "Hello"
meowclaw send ... --api-token "$MEOWCLAW_GATEWAY_API_TOKEN"   # when gateway.api_token is set
```

## API

### Health
```
GET /api/health
```

### Send Message
```
POST /api/send
Authorization: Bearer <gateway.api_token>   # if configured
Content-Type: application/json
{"channel": "telegram", "to": "123456", "text": "Hello"}
```

### WebSocket
```
ws://127.0.0.1:6820/ws?token=<gateway.api_token>   # if configured (browsers cannot set WS Authorization)
# or
ws://127.0.0.1:6820/ws

// Send
{"action": "send", "payload": {"channel": "webchat", "to": "...", "text": "..."}}

// Receive events
{"type": "message.received", "payload": {...}}
{"type": "agent.stream", "payload": {"delta": "Hello", "session_id": "...", "message_id": "..."}}
{"type": "agent.stream.end", "payload": {"session_id": "...", "message_id": "..."}}
{"type": "agent.response", "payload": {...}}
{"type": "channel.online", "payload": {...}}
```

## Building

```bash
# Development build
go build -o meowclaw ./cmd/meowclaw

# Production build (with FTS5 + stripped symbols, ~20MB)
make build
```

## Project Structure

```
meowclaw/                          2,413 lines of Go across 15 files
├── cmd/meowclaw/main.go          -- CLI entry point (init, up, status, send)
├── internal/
│   ├── gateway/
│   │   ├── gateway.go            -- HTTP + WebSocket gateway server
│   │   ├── hub.go                -- Client management + message routing
│   │   └── client.go             -- WebSocket client with ping/pong keepalive
│   ├── channel/
│   │   ├── channel.go            -- Channel interface (Start/Stop/Send/Receive/Health)
│   │   ├── telegram/telegram.go  -- Telegram bridge (go-telegram-bot-api)
│   │   ├── discord/discord.go    -- Discord bridge (discordgo)
│   │   ├── whatsapp/whatsapp.go  -- WhatsApp bridge (whatsmeow, Go-native)
│   │   ├── slack/slack.go        -- Slack bridge (slack-go, Socket Mode)
│   │   └── webchat/webchat.go    -- Built-in WebChat pass-through
│   ├── agent/
│   │   ├── agent.go              -- LLM agent runtime with session management
│   │   └── provider/
│   │       ├── provider.go       -- Provider interface
│   │       ├── openai.go         -- OpenAI Chat Completions (stream + non-stream)
│   │       ├── anthropic.go      -- Anthropic Messages API
│   │       └── urls.go           -- Default API paths + base/path join helper
│   ├── memory/memory.go          -- SQLite + FTS5 persistent memory & search
│   ├── config/config.go          -- YAML config loader
│   └── message/message.go        -- Unified message types & event constants
├── web/static/index.html         -- Dashboard UI (dark theme, real-time stats)
├── Makefile                      -- build / run / test / install
├── config.example.yaml
└── .gitignore
```

## Key Advantages over OpenClaw

| | OpenClaw | MeowClaw |
|---|---|---|
| Binary | npm + Node 24 (~200MB+) | **~20MB single binary** |
| Install | `npm install -g` + complex config | **`make build`** + **`meowclaw init`** |
| WhatsApp | Baileys (JS, unstable, no keepalive) | **whatsmeow (Go-native, keepalive, WAL store)** |
| Memory | Sessions often ephemeral | **SQLite + FTS5** (search + persistence) |
| Stability | Restarts ~50min, OOM reports | **Goroutine isolation**, bounded memory profile |
| Slack | Not a first-class story here | **Socket Mode** (no public URL) |
| AI | Often OpenAI-first | **OpenAI + Anthropic**, **SSE streaming**, **`/model` per chat** |
| Gateway | Broad attack surface if exposed | **Loopback default**, optional **API token**, **Origin allowlist** |
| Config | Large JSON | **Small YAML** + **`base_url` / API path** overrides without rebuild |
| Audit surface | Large TS monorepo | **Compact Go tree** — agent, gateway, channels in one module |

### WhatsApp Stability

OpenClaw's #1 user complaint is WhatsApp disconnects ([#4686](https://github.com/openclaw/openclaw/issues/4686)).
Root causes identified by the community:

1. **No presence keepalives** — WhatsApp kills idle sessions after ~24h
2. **Single-file credential backup** — corrupt on restart = session lost
3. **Blind reconnection** — all disconnect reasons treated the same

MeowClaw addresses all three:

- **whatsmeow** (Go-native WhatsApp client) instead of Baileys (JS reverse-engineering)
- **4-minute keepalive pings** to prevent session timeout
- **SQLite WAL-mode credential storage** with proper journal for crash safety
- **Per-channel goroutine isolation** — one channel crash never takes down the gateway

## Roadmap

### v0.2 — Stability & Core Channels
- [x] Slack channel bridge (`slack-go`) ✅
- [x] Streaming responses (chunked WebSocket delivery) ✅
- [x] Chat commands: `/new`, `/reset`, `/status`, `/model` ✅
- [x] Auto-reconnect with backoff for all channels ✅
- [ ] Dockerfile + Docker Compose for one-command deploy
- [ ] Goreleaser for cross-platform binaries (Linux/macOS/Windows)

### v0.3 — More Providers & Automation
- [ ] Gemini API provider (Google)
- [ ] Ollama / local model support (run without cloud API keys)
- [ ] Cron / scheduled messages
- [ ] Webhook inbound (receive events from external services)
- [ ] Rate limiting per session
- [ ] DM pairing & allowlist (security, like OpenClaw's pairing system)

### v0.4 — Multi-user & Persistence
- [ ] Multi-user role-based access control (RBAC)
- [ ] Session compaction (summarize long conversations to save tokens)
- [ ] Vector embedding search for memory (semantic recall)
- [ ] Export/import conversation history (JSON, Markdown)
- [ ] Per-channel message chunking (long messages split for Telegram/Discord limits)

### v1.0 — Product
- [ ] Skill / plugin system (load `.so` or WASM extensions)
- [ ] MeowClaw Cloud — managed hosting service
- [ ] Tailscale Serve/Funnel support for remote access
- [ ] Control UI with session management & cost tracking
- [ ] i18n (Korean, Japanese, Chinese, German)
- [ ] `meowclaw doctor` — self-diagnosis tool

## Why "MeowClaw"?

Built on top of **whatsmeow**, the Go-native WhatsApp library.
OpenClaw is a lobster — big, heavy, complex.
MeowClaw is a cat — light, agile, and survives anything. 🐱

## Contributing

AI/vibe-coded PRs welcome!

## License

MIT
