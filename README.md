# LAM-Router

[![npm version](https://img.shields.io/npm/v/lam-router)](https://www.npmjs.com/package/lam-router)
[![npm downloads](https://img.shields.io/npm/dm/lam-router)](https://www.npmjs.com/package/lam-router)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Lightweight, ultra-high-throughput Autonomous AI Routing Gateway and Management Dashboard. Engineered in Go with an embedded React 19 PWA interface, native SQLite WAL persistence, intelligent failover combos, SHA-256 response caching, and zero-touch provider session bridges.

Ships as a standalone native binary and installs from npm — no clone, no Go toolchain, no build step:

```bash
npm install -g lam-router
lam-router
```

---

## ⚡ Key Highlights & Capabilities

### 1. High-Throughput Core & Sub-Millisecond Response Caching
* **Extreme Performance:** 32,000+ RPS peak throughput with minimal memory footprint (~40 MB RAM).
* **SHA-256 Response Cache:** In-memory instant caching (~1ms latency) for repeated deterministic queries to dramatically slash token costs.
* **Universal SSE Synthesizer:** Real-time bi-directional Server-Sent Events (`text/event-stream`) streaming translation across OpenAI, Anthropic, and Gemini protocols.

### 2. Antigravity Anti-Ban Decoy & Cloaking System
* **21 Official IDE Decoy Tools:** Masks autonomous agent operations using IDE-native signatures (`run_command`, `replace_file_content`) with protobuf safeguards.
* **Anti-Competitive Prompt Sanitization:** Strips conflicting identity headers and prompts to avoid synthetic rate-limiting.

### 3. Dynamic Egress Proxy Pools & Edge Relays
* **IP Rotation:** Built-in support for HTTP, HTTPS, and SOCKS5 proxy pools.
* **Edge Relays:** Supports Vercel, Cloudflare, and Deno edge relays (`x-relay-target` / `x-relay-path`) for distributed ingress/egress.

### 4. Embedded Full PWA Dashboard & Playground
* **Zero Runtime Node.js Dependency:** Production Go binary embeds the pre-built React frontend via `embed.FS` on a single unified port (port 9898).
* **Interactive AI Playground:** Test models live with adjustable temperature, max tokens, system prompts, thinking trace toggles, and multi-language code export.
* **Live Telemetry & Topology Bus Map:** Visualizes real-time request conduits, token savings, throughput, and connection status.
* **2-Tier Data Portability:** Independent JSON backup/restore for (1) UI preferences and (2) Gateway database accounts.

---

## 🏗️ Architecture Overview

```
┌────────────────────────────────────────────────────────┐
│                   Client Applications                  │
│    Hermes Agent • Claude Code • OpenCode • Python SDK  │
└───────────────────────────┬────────────────────────────┘
                            │ (OpenAI-compatible /v1/*)
                            ▼
┌────────────────────────────────────────────────────────┐
│                    LAM-Router Core                     │
│  ┌──────────────────────┬───────────────────────────┐  │
│  │ Ingress & Auth Gate  │ SHA-256 Response Cache    │  │
│  ├──────────────────────┼───────────────────────────┤  │
│  │ Capability Switcher  │ Combo Load Balancer       │  │
│  ├──────────────────────┼───────────────────────────┤  │
│  │ MITM Tool Cloaking   │ Stream SSE Synthesizer    │  │
│  └──────────────────────┴───────────────────────────┘  │
│         │                                    │         │
│         ▼                                    ▼         │
│  Embedded PWA Dashboard             SQLite WAL Database│
│  (Port 9898 / Webdist)             (~/.lam-router/db)  │
└───────────────────────────┬────────────────────────────┘
                            │ (Secure HTTPS / Proxy Pools)
                            ▼
┌────────────────────────────────────────────────────────┐
│                   Upstream AI Mesh                     │
│         Claude • Gemini • OpenAI • Custom APIs         │
└────────────────────────────────────────────────────────┘
```

---

## 🚀 Installation

### Install from npm (recommended)

All you need is Node.js 18+ — nothing else:

```bash
npm install -g lam-router
```

The install provisions the standalone native binary for your platform. Run it without installing first, if you prefer:

```bash
npx lam-router tui   # interactive terminal dashboard
npx lam-router       # start the gateway service
```

> [!IMPORTANT]
> **There is nothing to clone or compile.** The published package carries (or fetches) a pre-built Go binary with the dashboard already embedded. Source builds are only needed if you want to change the code — see [Building From Source](#-building-from-source-contributors).

### Platform Support

| Platform | Architecture | How the binary arrives |
| :--- | :--- | :--- |
| **Linux** | x64, arm64 | platform package from npm (`lam-router-linux-*`) |
| **macOS** | Apple Silicon, Intel | platform package from npm (`lam-router-darwin-*`) |
| **Windows** | x64 | npm refuses the `lam-router-win32-x64` package name (spam screening), so the launcher downloads `lam-router-windows-amd64.exe` from the matching GitHub Release, verifies its SHA-256 against the release `checksums.txt`, then caches and runs it. The download happens once per version; set `LAM_ROUTER_NO_DOWNLOAD=1` to keep the launcher offline-only. |

These are the only supported targets. Node acts purely as the delivery vehicle: the gateway itself is a single self-contained Go binary — no Node runtime, no shared libraries, no dependency install at runtime.

### 📋 Prerequisites & Requirements

| Tool / Dependency | Required? | Min Version | Purpose | Installation / Download |
|---|---|---|---|---|
| **[Node.js](https://nodejs.org/) & npm** | ✅ Required | Node 18+ | Installs and launches the gateway binary | [Node.js Download](https://nodejs.org/) |
| **[PM2](https://pm2.keymetrics.io/)** | ⭐ Optional | latest | 24/7 background process manager (survives reboots) | `npm install -g pm2` ([PM2 Quickstart](https://pm2.keymetrics.io/docs/usage/quick-start/)) |
| **[Go (Golang)](https://go.dev/doc/install)** + **[Make](https://www.gnu.org/software/make/)** | 🧰 Source build only | Go 1.22+ | Compiles the binary yourself (contributors / unreleased code) | [Go Install Guide](https://go.dev/doc/install) · Debian/Ubuntu: `sudo apt install build-essential` |
| **[pnpm](https://pnpm.io/)** | ⚙️ UI dev only | 9+ | Rebuilds the embedded React dashboard | [pnpm Install](https://pnpm.io/installation) |

---

## ▶️ Running LAM-Router

### 1. Directly from the terminal

```bash
lam-router        # start the gateway
lam-router tui    # interactive terminal dashboard (aliases: menu, ui)
```

By default, the server starts on `http://127.0.0.1:9898`. Open that URL to reach the dashboard and finish the first-run setup.

### 2. As a 24/7 background service with PM2

Point PM2 at the **native binary**, not at the npm launcher: `bin/cli.js` only spawns the real process and does not forward signals, so `pm2 start lam-router` would manage the wrong PID.

```bash
BIN="$(lam-router version | sed -n 's/^Binary path: *//p')"   # path of the real binary
pm2 start "$BIN" --name lam-router
pm2 save
```

`lam-router version` prints the exact binary path for your install (under `lib/node_modules/lam-router/node_modules/lam-router-<platform>/bin/`). On Windows, PM2 should point at the cached `lam-router.exe` path printed there.

### 3. As a systemd service

Create `/etc/systemd/system/lam-router.service`:

```ini
[Unit]
Description=LAM-Router AI Gateway
After=network.target

[Service]
Type=simple
User=<your_username>
WorkingDirectory=/home/<your_username>
ExecStart=<path printed by `lam-router version`>
Restart=always
RestartSec=5
Environment=PORT=9898
Environment=DATA_DIR=/home/<your_username>/.lam-router

[Install]
WantedBy=multi-user.target
```

Enable and start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now lam-router
```

---

## 🔄 Updating

Updates are installed by npm, the same way the binary was installed — LAM-Router never overwrites its own binary:

```bash
lam-router update         # checks npm, then runs `npm i -g lam-router@latest`
# or manually:
npm i -g lam-router@latest

pm2 restart lam-router    # or: systemctl restart lam-router
```

`lam-router version` reports which channel owns the running binary (`npm` for registry installs, `manual` for source builds), the resolved binary path, and the newest release on npm. Background update checks are logged at startup; set `UPDATE_CHECK=false` to silence them.

Uninstalling is the npm inverse — `npm uninstall -g lam-router`. Your data stays in `DATA_DIR` (`~/.lam-router`), so reinstalling keeps accounts, keys, and request logs.

---

## 🧰 Building From Source (contributors)

Only needed to run unreleased code or modify the gateway/UI:

```bash
git clone https://github.com/ahlfs/LAM-Router.git
cd LAM-Router
make install          # compiles the binary (with embedded dashboard) into $PATH
```

Source installs report the `manual` install channel in `lam-router version` and are updated by rebuilding (`git pull && make install`), not by `lam-router update`. Rebuilding the embedded dashboard additionally needs Node 20+ and pnpm (`make build-ui`).

---

## 🌐 Reverse Proxy Configuration (Caddy / Nginx)

### Option A: Caddy (`/etc/caddy/Caddyfile`)
```caddy
router.yourdomain.com {
    reverse_proxy 127.0.0.1:9898 {
        header_up Host {upstream_hostport}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-Proto https
        flush_interval -1
    }
}
```
*Note: `flush_interval -1` is essential for instantaneous SSE token streaming.*

### Option B: Nginx (`/etc/nginx/sites-available/lam-router`)
```nginx
server {
    server_name router.yourdomain.com;

    location / {
        proxy_pass http://127.0.0.1:9898;
        proxy_http_version 1.1;
        proxy_set_header Connection '';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Disable buffering for real-time SSE streaming
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;
    }
}
```

---

## ⚙️ Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `LAM_ROUTER_PORT` | `9898` | HTTP listen port for gateway and dashboard (aliases, checked in order: `LAMROUTER_PORT`, `ROUTER_PORT`, `PORT`) |
| `DATA_DIR` | `~/.lam-router` (`%APPDATA%\lam-router` on Windows) | Base storage directory for the database, secrets and logs |
| `DB_PATH` | `$DATA_DIR/db/data.sqlite` | SQLite database file location |
| `INITIAL_PASSWORD` | *(empty)* | Sets the dashboard admin password on first run. No default is shipped: leave it empty and the first run guides you through creating the account |
| `LAM_ADMIN_PASSWORD` | *(empty)* | Re-seeds the admin password when no admin account exists yet (alias: `LAMROUTER_ADMIN_PASSWORD`) |
| `JWT_SECRET` | auto-generated → `$DATA_DIR/jwt-secret` | Secret used to sign dashboard sessions. Generated on first run with `crypto/rand` if unset |
| `LOG_LEVEL` | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`) |
| `LOG_FILE` | *(empty → stdout)* | Redirect logs to a file instead of stdout |
| `RTK_ENABLED` | `true` | RTK input compression |
| `CAVEMAN_ENABLED` | `false` | Caveman terse output style |
| `PONYTAIL_ENABLED` | `false` | Ponytail lazy-dev code style |
| `UPDATE_CHECK` | `true` | Check npm for a newer release shortly after startup and log it |
| `INJECTION_GUARD_DISABLED` | `false` | Disable the prompt-injection detector |
| `HTTP_PROXY` / `HTTPS_PROXY` | *(empty)* | Outbound proxy used for upstream provider calls |
| `LAMROUTER_LIVE_QUOTA_TTL` | `20` | Seconds a live Antigravity quota probe counts as fresh on the Quotas & Limits page: within it the page is answered from cache, past it cached quotas are served instantly and refreshed in the background (stale-while-revalidate, ceiling 10 minutes). `0` disables the cache so every request probes upstream |

A `.env` file is also read at startup, in this order: `./.env`, `~/LAM-Router/.env`, `~/.lam-router/.env`, `~/.hermes/.env`. Real environment variables always win over `.env` values. The three token savers can also be toggled per-run with `--rtk`, `--caveman`, `--ponytail`, and the injection guard with `--no-injection-guard`.

---

## 🔐 Authentication

Every `/v1/*` request must carry an API key created in the dashboard (**Endpoint & Keys**):

```bash
-H "Authorization: Bearer <your-lam-key>"   # or: -H "X-API-Key: <your-lam-key>"
```

Keys in query strings are deliberately rejected — they would leak through browser history, `Referer` headers, and upstream proxy logs. Authenticated dashboard sessions (the `lamrouter_admin_session` cookie) are additionally accepted, which is what powers the in-app Playground and Benchmarks. Keys can carry token quotas and credit limits, and an exhausted key is refused with `429`.

---

## 🔌 Connecting Clients & AI Frameworks

LAM-Router exposes a standard OpenAI-compatible API endpoint at `/v1`.

### 1. cURL Example
```bash
curl -X POST https://router.yourdomain.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ***" \
  -d '{
    "model": "antigravity/claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Explain quantum computing in one sentence."}
    ],
    "stream": true
  }'
```

### 2. Python (OpenAI SDK)
```python
from openai import OpenAI

client = OpenAI(
    base_url="https://router.yourdomain.com/v1",
    api_key="YOUR_LAM_API_KEY"
)

response = client.chat.completions.create(
    model="antigravity/claude-3-7-sonnet",
    messages=[{"role": "user", "content": "Hello LAM-Router!"}],
    stream=True
)

for chunk in response:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

### 3. Hermes Agent Setup
In `~/.hermes/config.yaml`:
```yaml
model:
  default_provider: "lam_router"

custom_providers:
  lam_router:
    base_url: "https://router.yourdomain.com/v1"
    api_key: "YOUR_LAM_API_KEY"
    models:
      - "river/deepseek-ai/DeepSeek-V4.1-Flash"
      - "antigravity/claude-3-7-sonnet"
```

---

## 📄 License

Distributed under the **MIT License**. See [LICENSE](LICENSE) for more details.

© 2026 **Muhamad Alamsyah Ahlul Firdaus (ahlfs)**. All rights reserved.
