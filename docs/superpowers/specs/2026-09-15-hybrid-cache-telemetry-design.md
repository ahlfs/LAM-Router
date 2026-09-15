# Design Spec: Hybrid Token Efficiency & Cache Telemetry for LAM-Router

- **Date:** 2026-09-15
- **Author:** Misa Amane & Alam
- **Status:** Approved / In Progress
- **Target Subsystem:** LAM-Router Backend (`proxy/internal`) & Frontend Dashboard (`srouter/apps/web`)

---

## 1. Problem Statement & Root Cause
Dashboard LAM-Router menampilkan `Tokens Saved: 0` meskipun total token usage mencapai lebih dari 700M token.
- **Root Cause 1:** Dashboard KPI card membaca in-memory exact-match cache (`MemoryCache.saved`) dari `GET /v1/settings/cache` yang reset ke 0 setiap kali process restart.
- **Root Cause 2:** Request berbasis coding agent (Hermes, Claude Code, Codex) memiliki history percakapan unik di setiap request, sehingga rasio exact-match cache hit mendekati 0%.
- **Root Cause 3:** Upstream prompt caching (Gemini & Claude prefix caching) sebenarnya telah menghemat **174.884.805 tokens** (~24.2% dari total prompt tokens) dan tersimpan di database SQLite (`request_logs` & `usageHistory`), namun belum pernah di-aggregate atau ditampilkan pada dashboard.
- **Root Cause 4:** RTK Tool compression (`tokensaver.CompressMessages`) belum mencatat delta token penghematan ke dalam database request telemetry.

---

## 2. Architectural Solution: Hybrid Token Efficiency System

Sistem efisiensi token baru menggabungkan 3 pilar penghematan menjadi satu kesatuan real-time:
1. **Upstream Prefix Cache (`cached_tokens`):** Token yang berhasil di-cache oleh provider upstream (Gemini KV cache / Anthropic prompt cache).
2. **RTK Message Compression (`compression_tokens`):** Token yang dihemat dari kompresi tool call outputs (git diff, grep, tree, file lists) sebelum dikirim ke upstream.
3. **Exact Response Cache Hits (`exact_cache_hits`):** Respons identik yang dilayani langsung dari memory cache 0ms.

---

## 3. Detailed Technical Design

### 3.1. Database Migration & Schema
- File: `internal/db/sqlite.go` / `internal/db/schema.go`
- Tambahkan kolom `compression_tokens INTEGER DEFAULT 0` pada tabel `request_logs`.
- Safe alter query saat database initialize:
  ```sql
  ALTER TABLE request_logs ADD COLUMN compression_tokens INTEGER DEFAULT 0;
  ```

### 3.2. Real-time Compression Token Tracking
- File: `internal/handlers/chat/fallback.go` & `internal/handlers/chat/usage.go`
- Di `applyTokenSavers(body []byte)`:
  - Rekam panjang byte sebelum kompresi RTK: `origLen := len(body)`
  - Jika `tokensaver.CompressMessages(body)` menghasilkan perubahan:
    - `compressionTokensSaved = (origLen - len(compressedBody)) / 4`
  - Teruskan nilai `compressionTokensSaved` ke `logUsage()`.
- Di `logUsage()`:
  - Simpan nilai `compression_tokens` pada `INSERT INTO request_logs`.

### 3.3. Telemetry Aggregation API
- File: `internal/handlers/srouterapi/logs_tunnel_oauth.go`
- Di endpoint `GET /v1/logs/stats`:
  - Query database SQLite:
    ```sql
    SELECT 
      COUNT(*), 
      COALESCE(SUM(total_tokens), 0), 
      COALESCE(SUM(prompt_tokens), 0), 
      COALESCE(SUM(completion_tokens), 0), 
      COALESCE(SUM(estimated_cost), 0), 
      COALESCE(AVG(latency_ms), 0),
      COALESCE(SUM(cached_tokens), 0),
      COALESCE(SUM(compression_tokens), 0)
    FROM request_logs;
    ```
  - Return JSON:
    ```json
    {
      "totalRequests": 12525,
      "totalTokens": 725336821,
      "totalInputTokens": 722127571,
      "totalOutputTokens": 3209250,
      "totalCachedTokens": 174884805,
      "totalCompressionTokens": 0,
      "totalSavedTokens": 174884805,
      "totalCost": 0.0,
      "costLabel": "$0.00",
      "estimated": true,
      "avgLatency": 15.2,
      "byModel": [...]
    }
    ```
- Di endpoint `GET /v1/settings/cache`:
  - Sertakan `totalUpstreamCachedTokens` dan `totalCompressionSavedTokens` dari SQLite agar UI Settings juga menampilkan status hybrid.

### 3.4. Frontend UI Components
- File 1: `apps/web/src/routes/index.tsx` (Dashboard Overview)
  - KPI Tile ke-3 diperbarui:
    - **Label:** `Tokens Saved`
    - **Value:** `formatCompactNumber(stats.totalSavedTokens)` (Contoh: `174.9M`)
    - **SubValue:** `formatCompactNumber(stats.totalCachedTokens) + " Upstream · " + formatCompactNumber(stats.totalCompressionTokens) + " RTK"`
    - **Detail / Tooltip:** Penjelasan lengkap efisiensi cache & kompresi.
- File 2: `apps/web/src/components/tokenSaver/ResponseCacheCard.tsx` (Token Saver Settings)
  - Tambahkan kartu telemetry untuk *Upstream Prefix Cache* & *Tool Output Compression* di samping *Exact Response Cache*.

---

## 4. Verification & Testing Plan
1. **Backend API Unit & Integration Tests:**
   - Jalankan `go test ./internal/handlers/chat/... ./internal/handlers/srouterapi/...`
   - Verifikasi output JSON dari `GET /v1/logs/stats` menghasilkan `totalSavedTokens > 0`.
2. **Frontend Build & Asset Sync:**
   - Jalankan `pnpm run build` di `srouter/apps/web`.
   - Sync ke `internal/webdist/dist/`.
   - Compile binary Go `make install`.
3. **End-to-End Live Verification:**
   - Restart PM2 service `pm2 restart lam-router`.
   - Verifikasi melalui curl / browser bahwa dashboard menampilkan angka ~174.9M token saved secara akurat dan tidak reset ke 0 saat service di-restart.
