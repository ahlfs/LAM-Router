# LAM-Router

Lightweight, ultra-high-throughput Autonomous AI Routing Gateway and Management Dashboard. Engineered in Go with an embedded React 19 PWA interface, native SQLite WAL persistence, intelligent failover combos, SHA-256 response caching, and zero-touch provider session bridges.

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
* **Zero Runtime Node.js Dependency:** Production Go binary embeds the pre-built React frontend via `embed.FS` on a single unified port (port 8000).
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
│  (Port 8000 / Webdist)             (~/.lam-router/db)  │
└───────────────────────────┬────────────────────────────┘
                            │ (Secure HTTPS / Proxy Pools)
                            ▼
┌────────────────────────────────────────────────────────┐
│                   Upstream AI Mesh                     │
│   Claude • Gemini • OpenAI • Framer AI • Custom APIs   │
└────────────────────────────────────────────────────────┘
```

---

## 🚀 Setup & Installation Guide

### Prerequisites
* **Go** >= 1.22 (for building backend)
* **Node.js** >= 20 & **pnpm** (for building frontend)
* **Linux / macOS / Windows**

---

### Step 1: Clone Repository

```bash
git clone https://github.com/ahlfs/LAM-Router.git
cd LAM-Router
```

---

### Step 2: Build & Install Globally

```bash
make install
```
*(Perintah ini akan mengompilasi binary mandiri dengan seluruh aset webdist ter-embed dan langsung memasangnya ke `$PATH` sistem. Setelah selesai, kamu bisa mengetik `lam-router` atau `lamrouter` langsung dari direktori mana saja di terminal!).*

---

### Step 3: Run LAM-Router

#### Running Directly from Terminal:
```bash
lam-router
```
By default, the server starts on `http://127.0.0.1:8000`.

#### Running as a 24/7 Background Service with PM2:
```bash
pm2 start lam-router --name "lam-router"
pm2 save
```

#### Running via Systemd Service:
Create `/etc/systemd/system/lam-router.service`:
```ini
[Unit]
Description=LAM-Router AI Gateway
After=network.target

[Service]
Type=simple
User=ahlfs
WorkingDirectory=/home/ahlfs/workspace/LAM-Router
ExecStart=/usr/local/bin/lam-router
Restart=always
RestartSec=5
Environment=PORT=8000
Environment=DATA_DIR=/home/ahlfs/.lam-router

[Install]
WantedBy=multi-user.target
```
Enable and start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now lam-router
```

---

### Step 4: Reverse Proxy Configuration (Caddy / Nginx)

#### Option A: Caddy (`/etc/caddy/Caddyfile`)
```caddy
router.yourdomain.com {
    reverse_proxy 127.0.0.1:8000 {
        header_up Host {upstream_hostport}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-Proto https
        flush_interval -1
    }
}
```
*Note: `flush_interval -1` is essential for instantaneous SSE token streaming.*

#### Option B: Nginx (`/etc/nginx/sites-available/lam-router`)
```nginx
server {
    server_name router.yourdomain.com;

    location / {
        proxy_pass http://127.0.0.1:8000;
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
| `PORT` | `8000` | HTTP listen port for gateway and dashboard |
| `BIND_ADDR` | `0.0.0.0` | Network interface binding address |
| `DATA_DIR` | `~/.lam-router` | Base storage directory for databases and logs |
| `DB_PATH` | `~/.lam-router/db/data.sqlite` | SQLite database file location |
| `REQUIRE_API_KEY` | `false` | When `true`, enforces valid API key for all `/v1/*` requests |
| `ADMIN_PASSWORD` | *(empty)* | Optional master password to lock the dashboard UI |
| `LOG_LEVEL` | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`) |

---

## 🔌 Connecting Clients & AI Frameworks

LAM-Router exposes a standard OpenAI-compatible API endpoint at `/v1`.

### 1. cURL Example
```bash
curl -X POST https://router.yourdomain.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_LAM_API_KEY" \
  -d '{
    "model": "framer/google/gemini-3-flash-preview",
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
      - "framer/google/gemini-3-flash-preview"
      - "antigravity/claude-3-7-sonnet"
```

---

## 📄 License

Distributed under the **MIT License**. See [LICENSE](LICENSE) for more details.

© 2026 **Muhamad Alamsyah Ahlul Firdaus (ahlfs)**. All rights reserved.
