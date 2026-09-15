# Hybrid Token Efficiency & Telemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement real-time hybrid token efficiency telemetry across LAM-Router Go proxy and React frontend, aggregating upstream prompt cache and RTK compression savings.

**Architecture:** Extend SQLite `request_logs` schema with `compression_tokens`, capture RTK savings delta in chat handler pipeline, update `/v1/logs/stats` and `/v1/settings/cache` APIs, and surface comprehensive telemetry in the React dashboard KPI tiles and Settings view.

**Tech Stack:** Go (Standard Library, Chi Router, Modernc SQLite), TypeScript / React 18 / Tailwind CSS, TanStack Query.

**Spec:** `/home/ahlfs/workspace/LAM-Router/docs/superpowers/specs/2026-09-15-hybrid-cache-telemetry-design.md`

## Global Constraints
- Do not kill or restart ports 3000, 20128, or 8900.
- All Git commits must be authored as `Muhamad Alamsyah Ahlul Firdaus <ahlulffirdaus@gmail.com>`.
- Frontend builds must be synced to both `internal/webdist/dist/` and `web/dist/`.
- Maintain full backward compatibility for existing API contracts.

---

### Task 1: Database Migration & Schema Extension

**Files:**
- Modify: `internal/db/schema.go`
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Update SQLite table schema definition & auto-migration**
Ensure `request_logs` schema includes `compression_tokens INTEGER DEFAULT 0` and add safe migration execution in database startup.

- [ ] **Step 2: Verify Go build and migration integrity**
Run: `go test ./internal/db/...`
Expected: PASS

---

### Task 2: Capture RTK Message Compression Token Delta in Chat Pipeline

**Files:**
- Modify: `internal/handlers/chat/fallback.go`
- Modify: `internal/handlers/chat/usage.go`
- Modify: `internal/handlers/chat/chat.go`

- [ ] **Step 1: Capture pre and post-compression byte size in `applyTokenSavers`**
Return `(processedBody []byte, compressionTokensSaved int)` from token saver flow.

- [ ] **Step 2: Pass `compressionTokensSaved` into `logUsage` and write to `request_logs`**
Update `logUsage()` to insert `compression_tokens` column in `request_logs`.

- [ ] **Step 3: Run unit tests for chat handlers**
Run: `go test ./internal/handlers/chat/... -v`
Expected: PASS

---

### Task 3: Expose Unified Efficiency Metrics in SRouter REST APIs

**Files:**
- Modify: `internal/handlers/srouterapi/logs_tunnel_oauth.go`
- Modify: `internal/handlers/srouterapi/cache.go`

- [ ] **Step 1: Update `HandleLogsStats` (`/v1/logs/stats`)**
Add `totalCachedTokens`, `totalCompressionTokens`, and `totalSavedTokens` to the SQL query and returned JSON payload.

- [ ] **Step 2: Update `HandleCacheGet` (`/v1/settings/cache`)**
Enrich cache stats with persistent database totals for hybrid efficiency visibility.

- [ ] **Step 3: Run unit tests for srouterapi**
Run: `go test ./internal/handlers/srouterapi/... -v`
Expected: PASS

---

### Task 4: Frontend Dashboard & Settings Telemetry Update

**Files:**
- Modify: `/home/ahlfs/workspace/LAM-Router-staging/srouter/apps/web/src/routes/index.tsx`
- Modify: `/home/ahlfs/workspace/LAM-Router-staging/srouter/apps/web/src/components/tokenSaver/ResponseCacheCard.tsx`
- Modify: `/home/ahlfs/workspace/LAM-Router-staging/srouter/packages/types/src/index.ts` (if required for UsageStats type)

- [ ] **Step 1: Update UsageStats types & hooks**
Add `totalCachedTokens`, `totalCompressionTokens`, `totalSavedTokens` to `UsageStats` interface.

- [ ] **Step 2: Update Dashboard KPI Tile in `apps/web/src/routes/index.tsx`**
Render unified `Tokens Saved` tile with compact numbers, breakdown subValue (`174.9M Upstream · 0 RTK`), and descriptive tooltips.

- [ ] **Step 3: Update `ResponseCacheCard.tsx`**
Display persistent upstream cache & RTK compression telemetry cards alongside exact memory cache.

- [ ] **Step 4: Build frontend assets and sync to proxy webdist**
Run: `cd /home/ahlfs/workspace/LAM-Router-staging/srouter/apps/web && pnpm run build`
Sync: `cp -r dist/* /home/ahlfs/workspace/LAM-Router/internal/webdist/dist/`

---

### Task 5: End-to-End Build, Deploy & Live Verification

- [ ] **Step 1: Compile binary and install globally**
Run: `cd /home/ahlfs/workspace/LAM-Router && make install`

- [ ] **Step 2: Restart PM2 process**
Run: `pm2 restart lam-router`

- [ ] **Step 3: Verify live API endpoint output**
Run: `curl -s http://localhost:9898/v1/logs/stats`
Expected: `totalSavedTokens` returns ~174,884,805.
