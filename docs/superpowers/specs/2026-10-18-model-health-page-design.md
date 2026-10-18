# Design Specification: Model Health & Latency Matrix Page

**Date:** 2026-10-18  
**Status:** Approved  
**Author:** Asa Mitaka & ahlfs  
**Target:** LAM-Router (`ahlfs/LAM-Router`)

---

## 1. Executive Summary

This feature adds an active **Model Health & Latency Matrix** subsystem to LAM-Router. Users can inspect the operational health (`HEALTHY`, `OFFLINE`, `PROBING`, `UNTESTED`), round-trip latency (ms), and upstream error diagnostics of every configured AI model. Probes can be executed individually per model or concurrently across all models with controlled concurrency to prevent upstream rate-limiting. Status and historical latency are persisted in the SQLite database so state is preserved across web dashboard refreshes.

The user interface is designed for the Web Dashboard with fully responsive desktop and mobile support, using SVG iconography (Lucide / vector SVG standards, no raw emojis).

---

## 2. Architecture & Components

```
┌────────────────────────────────────────────────────────┐
│             Web Dashboard (React / Vite)              │
│       /health Route (Responsive Desktop & Mobile)      │
└───────────────────────────┬────────────────────────────┘
                            │ REST API (/v1/health/models)
┌───────────────────────────▼────────────────────────────┐
│                  LAM-Router Backend                    │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │ routerapi/health.go (HTTP Handler & Worker Pool) │  │
│  └───────────────────────┬──────────────────────────┘  │
│                          │                             │
│       ┌──────────────────┴──────────────────┐          │
│       ▼                                     ▼          │
│  ┌───────────────┐                  ┌───────────────┐  │
│  │ SQLite DB     │                  │ Chat Pipeline │  │
│  │(model_health) │                  │(max_tokens:1) │  │
│  └───────────────┘                  └───────┬───────┘  │
└─────────────────────────────────────────────┼──────────┘
                                              ▼
                             Upstream AI Providers
                             (Antigravity, OpenRouter, etc.)
```

---

## 3. Data Schema & Persistence

A dedicated SQLite table `model_health` stores the health status and latency metrics:

```sql
CREATE TABLE IF NOT EXISTS model_health (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    status TEXT NOT NULL,          -- 'healthy', 'offline', 'probing', 'untested'
    latency_ms INTEGER DEFAULT 0,  -- Round-trip time in milliseconds
    status_code INTEGER DEFAULT 0, -- HTTP response code (e.g. 200, 429, 401, 500)
    last_error TEXT,               -- Upstream error message or rejection reason
    last_checked_at INTEGER NOT NULL
);
```

### State Definitions
- `HEALTHY`: Model successfully responded with an HTTP 200 completion chunk.
- `OFFLINE`: Upstream responded with an HTTP client/server error (e.g., 401, 403, 429, 500) or connection timed out.
- `PROBING`: Model is currently actively executing a probe request.
- `UNTESTED`: Model has not been tested since reset or discovery.

---

## 4. Backend Probing Mechanism

### 4.1. Probe Payload
Probes send an ultra-minimal non-streaming chat completion to verify credentials, model validity, and pipeline routing without excessive token expenditure:

```json
{
  "model": "<model_id>",
  "messages": [
    {
      "role": "user",
      "content": "ping"
    }
  ],
  "max_tokens": 1,
  "stream": false
}
```

### 4.2. Execution Path
- Probing runs internally via the core chat handler (`ChatHandler.HandleChatCompletions`) using an in-memory `httptest.ResponseRecorder`.
- Timeout is hard-capped at 10 seconds per probe.
- Latency is measured from request dispatch to response arrival (`time.Since(start).Milliseconds()`).
- On HTTP 200:
  - `status = "healthy"`
  - `latency_ms = duration`
  - `last_error = ""`
- On HTTP != 200 or timeout:
  - `status = "offline"`
  - `status_code = resp.StatusCode`
  - `last_error = <extracted error message>`

### 4.3. Concurrency & Rate Limiting
- When "Test All Models" is triggered, jobs are queued into a worker pool with a maximum concurrency of 3–4 workers.
- This prevents sudden bursts of outbound requests that could trigger quota exhaustion or temporary IP bans from providers.

---

## 5. REST API Endpoints

All endpoints are mounted under `/v1/health/models` and registered in `routerapi.RegisterV1Routes`:

### `GET /v1/health/models`
Returns the status of all configured models, joining provider metadata with health records:
```json
{
  "models": [
    {
      "model_id": "antigravity/claude-3-7-sonnet",
      "provider_id": "antigravity",
      "provider_name": "Google Antigravity",
      "status": "healthy",
      "latency_ms": 182,
      "status_code": 200,
      "last_error": null,
      "last_checked_at": 1792312000
    }
  ],
  "stats": {
    "total": 42,
    "healthy": 38,
    "offline": 2,
    "untested": 2,
    "avg_latency_ms": 210
  }
}
```

### `POST /v1/health/models/probe`
Triggers an immediate probe for a single model or all models:
- Single probe request body: `{"model_id": "antigravity/claude-3-7-sonnet"}`
- All models request body: `{"all": true}`
- Returns updated model status object(s).

### `POST /v1/health/models/reset`
Resets all health records to `untested` and clears error messages.

---

## 6. Frontend UI & UX (Web Dashboard)

### 6.1. Navigation
- Added to the primary sidebar / navbar navigation:
  - Path: `/health`
  - Label: `Model Health`
  - Icon: SVG Activity Pulse / Heartbeat (SVG vector, matching Lucide aesthetic).

### 6.2. Header & Metrics Bar
- Global title: `Model Health & Latency Matrix` with subtitle `Live upstream latency & inference health check`.
- Action buttons:
  - Primary button: `[⚡ Test All Models]` (triggers background concurrent probe with progress indicator).
  - Secondary button: `[Reset Records]` (resets status to Untested).
- Summary KPI Cards:
  - Total Models Monitored (Blue/Neutral)
  - Healthy Models (Emerald/Green)
  - Offline Models (Rose/Red)
  - Average Latency (Indigo/Amber)

### 6.3. Filtering & Search
- Quick status filters: `All`, `Healthy`, `Offline`, `Untested`.
- Provider filter dropdown: `All Providers`, `Antigravity`, `OpenRouter`, etc.
- Search input: Real-time filtering by model name or provider ID.

### 6.4. Responsive Layout (Mobile & Desktop)
- **Desktop (>= 768px):** Clean table view with Provider, Model Name, Status Badge, Latency Badge (color-coded: <300ms green, 300-800ms yellow, >800ms orange), Last Checked timestamp, and individual `[Test]` action button. Error reasons expand on row click or hover tooltip.
- **Mobile (< 768px):** Responsive card stack view:
  - Each model is displayed as an individual card with provider logo, model name, status badge, latency badge, last checked time, and full-width `[Test]` button.
  - Zero horizontal scroll; touch targets are at least 44px for optimal mobile usability.

### 6.5. Iconography Standard
- Strict compliance: Zero raw emojis in UI templates.
- SVG vectors only (Lucide SVG or inline SVG icons) for all buttons, indicators, and status badges:
  - Activity / Heartbeat: `ActivityIcon` SVG
  - Check / Healthy: `CheckCircle2` SVG
  - Error / Offline: `AlertCircle` SVG
  - Testing / Probing: `Loader2` SVG with spin animation
  - Untested: `HelpCircle` SVG

---

## 7. Error Handling & Edge Cases

1. **Provider Disconnected / Missing Credentials:**
   - Probe fails immediately without network hang.
   - Status marked as `OFFLINE` with error `Provider connection missing or disabled`.
2. **Upstream Rate Limit (HTTP 429):**
   - Status marked as `OFFLINE` with specific badge `RATE LIMITED (429)`.
   - Error snippet extracted and displayed to user.
3. **Upstream Timeout (> 10s):**
   - Probe context cancelled automatically at 10 seconds.
   - Status marked as `OFFLINE` with error `Connection timed out (10s exceeded)`.
4. **Concurrent Re-probe Protection:**
   - If a model is already in `PROBING` state, duplicate probe requests for the same model are debounced / ignored.

---

## 8. Verification & Testing Plan

1. **Unit Tests (Go Backend):**
   - Test table schema migration and queries in `internal/handlers/routerapi/health_test.go`.
   - Test `ProbeModel` with mocked upstream responses (200 OK, 429 Rate Limit, 500 Error, Timeout).
   - Test concurrent worker pool with N models.
2. **Integration Verification:**
   - Verify `GET /v1/health/models` returns correct structure.
   - Verify `POST /v1/health/models/probe` executes and updates SQLite table.
   - Verify frontend `/health` renders correctly on both desktop and mobile viewports.
   - Verify all unit tests pass (`go test ./...`).
