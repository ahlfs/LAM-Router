# lam-router

> **Ultra-fast AI API Proxy Gateway with Multi-Provider Failover, Virtual Combos, Token Savers, and Interactive Terminal UI.**

## 🚀 Quick Start

Run directly without installing via `npx`:

```bash
# Launch interactive Terminal UI dashboard
npx lam-router tui

# Or start the background router service
npx lam-router
```

Or install globally:

```bash
npm install -g lam-router

# Now you can use either 'lam-router' or 'lamrouter' anywhere:
lam-router tui
lam-router --help
```

---

## ✨ Features

- **Interactive Terminal UI (TUI):** Built with Charmbracelet Bubble Tea & Lipgloss. Full terminal responsiveness, live provider metrics, model tester, API key management, and failover monitoring.
- **Multi-Provider Failover & Virtual Combos:** Cascade requests across Antigravity (Google CloudCode), OpenAI, Claude, DeepSeek, Groq, Mistral, and custom endpoints with zero downtime.
- **Smart Quota & Error Recovery:** Auto-switches connections on HTTP 429 (Rate Limit) and cleans thinking budgets / parameter quirks on HTTP 400.
- **Token Savers:** Built-in prompt caching, RTK compression, Caveman terse mode, and Ponytail optimization.
- **Zero Configuration:** Single standalone native binary automatically provisioned for your platform (Linux x64/ARM64, macOS Apple Silicon/Intel, Windows x64).

---

## 🛠️ Commands

| Command | Description |
|---------|-------------|
| `lam-router` | Start the local proxy server on port `9898` |
| `lam-router tui` | Open the interactive Charmbracelet Terminal UI dashboard |
| `lam-router version` | Show the running version, install channel and npm release status |
| `lam-router update` | Install the newest release via npm (npm-owned installs only) |
| `lam-router mitm status` | View MITM proxy interception status |

---

## 🔄 Updating

LAM-Router never overwrites its own binary. Updates are installed by npm, the
same way the binary was installed:

```bash
lam-router update          # checks npm, then runs `npm i -g lam-router@latest`
# or manually:
npm i -g lam-router@latest

pm2 restart lamrouter      # or: systemctl restart lam-router
```

`lam-router version` reports which channel owns the running binary
(`npm` when it was installed from the registry, `manual` for source builds) and
prints the exact command to run. Background versions are logged at startup; set
`UPDATE_CHECK=false` to silence that notice.

---

## 📄 License

MIT © [ahlfs](https://github.com/ahlfs/LAM-Router)
