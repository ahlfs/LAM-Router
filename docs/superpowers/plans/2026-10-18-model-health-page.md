# Model Health & Latency Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete Model Health & Latency Matrix page in LAM-Router, enabling users to monitor operational health (`HEALTHY`, `OFFLINE`, `PROBING`, `UNTESTED`), round-trip latency (ms), and upstream error diagnostics across all configured AI models with controlled concurrent probing, SQLite persistence, and a mobile-responsive UI with SVG icons.

**Architecture:** A dedicated SQLite table `model_health` stores probe records. An internal probe worker sends lightweight non-streaming chat completions (`max_tokens: 1`) via `httptest.ResponseRecorder` against the chat pipeline with a 10s timeout. A bounded worker pool (3-4 workers) handles bulk probing. REST endpoints (`/v1/health/models`, `/v1/health/models/probe`, `/v1/health/models/reset`) serve the responsive React web dashboard.

**Tech Stack:** Go (chi router, SQLite database/sql, httptest), React / Tailwind CSS (Lucide SVG icons, responsive flex/grid layouts).

**Spec:** `docs/superpowers/specs/2026-10-18-model-health-page-design.md`

## Global Constraints
- Strict Iconography: Zero raw emojis in UI templates; use vector SVGs (Lucide style) exclusively.
- Responsive Design: Dual layout support (desktop table view, mobile stacked card view with >= 44px touch targets).
- Database Persistence: SQLite table `model_health` preserves status across web page refreshes.
- Probe Safety: Minimal non-streaming payload (`max_tokens: 1`), 10-second timeout, 3-4 worker pool concurrency.

---

### Task 1: Database Migration & Schema for Model Health

**Files:**
- Create: `internal/handlers/routerapi/health_db.go`
- Modify: `internal/handlers/routerapi/routes.go:18-218`
- Test: `internal/handlers/routerapi/health_db_test.go`

**Interfaces:**
- Produces:
  ```go
  type ModelHealthRecord struct {
      ModelID       string `json:"model_id"`
      ProviderID    string `json:"provider_id"`
      Status        string `json:"status"` // "healthy", "offline", "probing", "untested"
      LatencyMs     int64  `json:"latency_ms"`
      StatusCode    int    `json:"status_code"`
      LastError     string `json:"last_error,omitempty"`
      LastCheckedAt int64  `json:"last_checked_at"`
  }
  func (h *RouterAPIHandler) InitHealthSchema() error
  func (h *RouterAPIHandler) GetModelHealthRecords() ([]ModelHealthRecord, error)
  func (h *RouterAPIHandler) UpsertModelHealth(rec ModelHealthRecord) error
  func (h *RouterAPIHandler) ResetModelHealth() error
  ```

- [ ] **Step 1: Write the failing test for health DB schema and operations**
Create `internal/handlers/routerapi/health_db_test.go`:
```go
package routerapi

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestModelHealthDBOperations(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	if err := h.InitHealthSchema(); err != nil {
		t.Fatalf("InitHealthSchema failed: %v", err)
	}

	// Initial get should be empty
	records, err := h.GetModelHealthRecords()
	if err != nil {
		t.Fatalf("GetModelHealthRecords failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}

	// Upsert healthy model
	now := time.Now().Unix()
	rec := ModelHealthRecord{
		ModelID:       "antigravity/claude-3-7-sonnet",
		ProviderID:    "antigravity",
		Status:        "healthy",
		LatencyMs:     185,
		StatusCode:    200,
		LastError:     "",
		LastCheckedAt: now,
	}
	if err := h.UpsertModelHealth(rec); err != nil {
		t.Fatalf("UpsertModelHealth failed: %v", err)
	}

	records, err = h.GetModelHealthRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d (err: %v)", len(records), err)
	}
	if records[0].Status != "healthy" || records[0].LatencyMs != 185 {
		t.Errorf("unexpected record content: %+v", records[0])
	}

	// Reset
	if err := h.ResetModelHealth(); err != nil {
		t.Fatalf("ResetModelHealth failed: %v", err)
	}
	records, _ = h.GetModelHealthRecords()
	if len(records) != 1 || records[0].Status != "untested" {
		t.Errorf("expected status 'untested' after reset, got %+v", records)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/handlers/routerapi -run TestModelHealthDBOperations`
Expected: FAIL (compilation error: `InitHealthSchema` undefined).

- [ ] **Step 3: Implement `InitHealthSchema`, `GetModelHealthRecords`, `UpsertModelHealth`, and `ResetModelHealth`**
Create `internal/handlers/routerapi/health_db.go`:
```go
package routerapi

import (
	"database/sql"
	"time"
)

type ModelHealthRecord struct {
	ModelID       string `json:"model_id"`
	ProviderID    string `json:"provider_id"`
	Status        string `json:"status"` // healthy, offline, probing, untested
	LatencyMs     int64  `json:"latency_ms"`
	StatusCode    int    `json:"status_code"`
	LastError     string `json:"last_error,omitempty"`
	LastCheckedAt int64  `json:"last_checked_at"`
}

func (h *RouterAPIHandler) InitHealthSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS model_health (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    status TEXT NOT NULL,
    latency_ms INTEGER DEFAULT 0,
    status_code INTEGER DEFAULT 0,
    last_error TEXT,
    last_checked_at INTEGER NOT NULL
);
`
	_, err := h.db.Exec(schema)
	return err
}

func (h *RouterAPIHandler) GetModelHealthRecords() ([]ModelHealthRecord, error) {
	rows, err := h.db.Query(`
		SELECT model_id, provider_id, status, latency_ms, status_code, COALESCE(last_error, ''), last_checked_at
		FROM model_health
		ORDER BY model_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModelHealthRecord
	for rows.Next() {
		var rec ModelHealthRecord
		var lastErr string
		if err := rows.Scan(&rec.ModelID, &rec.ProviderID, &rec.Status, &rec.LatencyMs, &rec.StatusCode, &lastErr, &rec.LastCheckedAt); err != nil {
			return nil, err
		}
		rec.LastError = lastErr
		results = append(results, rec)
	}
	return results, rows.Err()
}

func (h *RouterAPIHandler) UpsertModelHealth(rec ModelHealthRecord) error {
	query := `
INSERT INTO model_health (model_id, provider_id, status, latency_ms, status_code, last_error, last_checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(model_id) DO UPDATE SET
    provider_id = excluded.provider_id,
    status = excluded.status,
    latency_ms = excluded.latency_ms,
    status_code = excluded.status_code,
    last_error = excluded.last_error,
    last_checked_at = excluded.last_checked_at;
`
	_, err := h.db.Exec(query, rec.ModelID, rec.ProviderID, rec.Status, rec.LatencyMs, rec.StatusCode, rec.LastError, rec.LastCheckedAt)
	return err
}

func (h *RouterAPIHandler) ResetModelHealth() error {
	now := time.Now().Unix()
	_, err := h.db.Exec(`UPDATE model_health SET status = 'untested', latency_ms = 0, status_code = 0, last_error = '', last_checked_at = ?`, now)
	return err
}
```

- [ ] **Step 4: Update `internal/handlers/routerapi/routes.go` to invoke `InitHealthSchema` inside `InitSchema`**
Edit `InitSchema(dbConn *sql.DB)` in `routes.go` to include the `model_health` table creation.

- [ ] **Step 5: Run tests and verify PASS**
Run: `go test -v ./internal/handlers/routerapi -run TestModelHealthDBOperations`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add internal/handlers/routerapi/health_db.go internal/handlers/routerapi/health_db_test.go internal/handlers/routerapi/routes.go
git commit -m "feat(backend): add model_health SQLite schema and persistence operations"
```

---

### Task 2: Backend Health Probe Engine & Worker Pool

**Files:**
- Create: `internal/handlers/routerapi/health_probe.go`
- Test: `internal/handlers/routerapi/health_probe_test.go`

**Interfaces:**
- Consumes: `ModelHealthRecord`, `UpsertModelHealth`
- Produces:
  ```go
  func (h *RouterAPIHandler) ProbeModel(ctx context.Context, modelID string) ModelHealthRecord
  func (h *RouterAPIHandler) ProbeAllModels(ctx context.Context, concurrency int) ([]ModelHealthRecord, error)
  ```

- [ ] **Step 1: Write test for ProbeModel and ProbeAllModels**
Create `internal/handlers/routerapi/health_probe_test.go`:
```go
package routerapi

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestProbeModel_Extraction(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	_ = h.InitHealthSchema()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Probing a nonexistent model should return offline status without crashing
	rec := h.ProbeModel(ctx, "mock/nonexistent-model")
	if rec.Status != "offline" {
		t.Errorf("expected offline status for nonexistent model, got %s", rec.Status)
	}
	if rec.LastCheckedAt == 0 {
		t.Errorf("expected non-zero LastCheckedAt")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/handlers/routerapi -run TestProbeModel_Extraction`
Expected: FAIL (compilation error: `ProbeModel` undefined).

- [ ] **Step 3: Implement `ProbeModel` and `ProbeAllModels` with Worker Pool**
Create `internal/handlers/routerapi/health_probe.go`:
```go
package routerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"lamrouter/internal/handlers/chat"
)

func (h *RouterAPIHandler) ProbeModel(ctx context.Context, modelID string) ModelHealthRecord {
	providerID := "custom"
	if strings.Contains(modelID, "/") {
		providerID = strings.Split(modelID, "/")[0]
	}

	payload := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req = req.WithContext(probeCtx)

	rec := httptest.NewRecorder()

	chatH := chat.NewChatHandler(h.repo, h.ts)
	start := time.Now()
	chatH.HandleChatCompletions(rec, req)
	latency := time.Since(start).Milliseconds()

	status := "healthy"
	errMsg := ""
	if rec.Code != http.StatusOK {
		status = "offline"
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err == nil {
			if errResp.Error.Message != "" {
				errMsg = errResp.Error.Message
			} else if errResp.Message != "" {
				errMsg = errResp.Message
			}
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	result := ModelHealthRecord{
		ModelID:       modelID,
		ProviderID:    providerID,
		Status:        status,
		LatencyMs:     latency,
		StatusCode:    rec.Code,
		LastError:     errMsg,
		LastCheckedAt: time.Now().Unix(),
	}

	_ = h.UpsertModelHealth(result)
	return result
}

func (h *RouterAPIHandler) ProbeAllModels(ctx context.Context, concurrency int) ([]ModelHealthRecord, error) {
	if concurrency <= 0 {
		concurrency = 3
	}

	allModels := h.getAllConfiguredModelIDs()
	if len(allModels) == 0 {
		return nil, nil
	}

	jobs := make(chan string, len(allModels))
	for _, m := range allModels {
		jobs <- m
	}
	close(jobs)

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]ModelHealthRecord, 0, len(allModels))

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for modelID := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					rec := h.ProbeModel(ctx, modelID)
					mu.Lock()
					results = append(results, rec)
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return results, nil
}

func (h *RouterAPIHandler) getAllConfiguredModelIDs() []string {
	// Retrieve models from providers and provider_models
	rows, err := h.db.Query(`SELECT DISTINCT model_id FROM provider_models WHERE is_active = 1`)
	if err == nil {
		defer rows.Close()
		var models []string
		for rows.Next() {
			var mid string
			if err := rows.Scan(&mid); err == nil && mid != "" {
				models = append(models, mid)
			}
		}
		if len(models) > 0 {
			return models
		}
	}

	// Fallback to active providers default list
	return []string{"antigravity/claude-3-7-sonnet", "antigravity/gemini-2.5-pro"}
}
```

- [ ] **Step 4: Run tests and verify PASS**
Run: `go test -v ./internal/handlers/routerapi -run TestProbeModel_Extraction`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/handlers/routerapi/health_probe.go internal/handlers/routerapi/health_probe_test.go
git commit -m "feat(backend): implement internal health probe engine with concurrent worker pool"
```

---

### Task 3: REST API Handlers & Routing

**Files:**
- Create: `internal/handlers/routerapi/health_handlers.go`
- Modify: `internal/handlers/routerapi/routes.go:325-397`
- Test: `internal/handlers/routerapi/health_handlers_test.go`

**Interfaces:**
- Produces:
  ```go
  func (h *RouterAPIHandler) HandleModelsHealthList(w http.ResponseWriter, req *http.Request)
  func (h *RouterAPIHandler) HandleModelsHealthProbe(w http.ResponseWriter, req *http.Request)
  func (h *RouterAPIHandler) HandleModelsHealthReset(w http.ResponseWriter, req *http.Request)
  ```

- [ ] **Step 1: Write integration tests for REST API endpoints**
Create `internal/handlers/routerapi/health_handlers_test.go`:
```go
package routerapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestHealthHandlers_E2E(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	_ = h.InitHealthSchema()

	// 1. GET /v1/health/models (initial empty/seeded)
	req := httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
	rec := httptest.NewRecorder()
	h.HandleModelsHealthList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 2. POST /v1/health/models/reset
	req = httptest.NewRequest(http.MethodPost, "/v1/health/models/reset", nil)
	rec = httptest.NewRecorder()
	h.HandleModelsHealthReset(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/handlers/routerapi -run TestHealthHandlers_E2E`
Expected: FAIL (compilation error: handlers undefined).

- [ ] **Step 3: Implement HTTP handlers in `internal/handlers/routerapi/health_handlers.go`**
Create `internal/handlers/routerapi/health_handlers.go`:
```go
package routerapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type HealthSummaryResponse struct {
	Models []ModelHealthRecord `json:"models"`
	Stats  HealthStats         `json:"stats"`
}

type HealthStats struct {
	Total        int   `json:"total"`
	Healthy      int   `json:"healthy"`
	Offline      int   `json:"offline"`
	Untested     int   `json:"untested"`
	AvgLatencyMs int64 `json:"avg_latency_ms"`
}

func (h *RouterAPIHandler) HandleModelsHealthList(w http.ResponseWriter, req *http.Request) {
	records, err := h.GetModelHealthRecords()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate stats
	stats := HealthStats{Total: len(records)}
	var sumLatency int64
	var healthyCount int64

	for _, r := range records {
		switch r.Status {
		case "healthy":
			stats.Healthy++
			sumLatency += r.LatencyMs
			healthyCount++
		case "offline":
			stats.Offline++
		default:
			stats.Untested++
		}
	}
	if healthyCount > 0 {
		stats.AvgLatencyMs = sumLatency / healthyCount
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthSummaryResponse{
		Models: records,
		Stats:  stats,
	})
}

func (h *RouterAPIHandler) HandleModelsHealthProbe(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		ModelID string `json:"model_id"`
		All     bool   `json:"all"`
	}
	_ = json.NewDecoder(req.Body).Decode(&reqBody)

	if reqBody.All {
		results, err := h.ProbeAllModels(req.Context(), 3)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"results": results,
		})
		return
	}

	if reqBody.ModelID == "" {
		http.Error(w, "model_id required", http.StatusBadRequest)
		return
	}

	res := h.ProbeModel(req.Context(), reqBody.ModelID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *RouterAPIHandler) HandleModelsHealthReset(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.ResetModelHealth(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Register routes in `internal/handlers/routerapi/routes.go`**
Add:
```go
r.HandleFunc("/v1/health/models", h.HandleModelsHealthList)
r.HandleFunc("/v1/health/models/probe", h.HandleModelsHealthProbe)
r.HandleFunc("/v1/health/models/reset", h.HandleModelsHealthReset)
```

- [ ] **Step 5: Run tests and verify PASS**
Run: `go test -v ./internal/handlers/routerapi -run TestHealthHandlers_E2E`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add internal/handlers/routerapi/health_handlers.go internal/handlers/routerapi/health_handlers_test.go internal/handlers/routerapi/routes.go
git commit -m "feat(api): register /v1/health/models REST endpoints for listing, probing, and resetting"
```

---

### Task 4: Web UI Integration — Navigation & Route Registration

**Files:**
- Modify: `web/dist/assets/index-yz9zKHrD.js` (and `internal/webdist/dist/assets/index-yz9zKHrD.js`)

**Interfaces:**
- Consumes: Nav items array in `index-yz9zKHrD.js`
- Produces: Navigation entry `{to:"/health",label:"Model Health",icon:HealthIconSVG}`

- [ ] **Step 1: Check existing navigation structure**
Identify navigation array declaration in `index-yz9zKHrD.js`.

- [ ] **Step 2: Append `/health` item with Lucide Activity SVG Icon**
Add `{to:"/health",label:"Model Health",icon:ActivityIcon}` to nav items.

- [ ] **Step 3: Test and Commit**
Verify router recognizes `/health` path without redirecting to 404.
```bash
git add web/dist/assets/index-yz9zKHrD.js internal/webdist/dist/assets/index-yz9zKHrD.js
git commit -m "feat(web): add Model Health tab to primary navigation"
```

---

### Task 5: Web UI Component — Model Health Matrix Page (Responsive & SVG Icons)

**Files:**
- Create: `web/dist/assets/health-matrix.js`
- Modify: Bundle loaders in `web/dist/assets/` to mount `/health` component
- Sync: `internal/webdist/dist/assets/`

**Interfaces:**
- Consumes: `GET /v1/health/models`, `POST /v1/health/models/probe`, `POST /v1/health/models/reset`
- Produces: Responsive UI with stats cards, filter controls, desktop table, mobile card stack, and individual/bulk probe buttons.

- [ ] **Step 1: Construct the Health Matrix Component**
Component features:
1. Lucide SVG icons only (Pulse, CheckCircle2, AlertCircle, RefreshCw, Loader2). Zero plain emojis.
2. Dual layout:
   - Mobile: `<div className="md:hidden space-y-3">` rendering touch-friendly cards.
   - Desktop: `<div className="hidden md:block">` rendering a detailed data table.
3. State management:
   - `models`: array of health records.
   - `stats`: total, healthy, offline, avg latency.
   - `search`: live filter string.
   - `statusFilter`: "all" | "healthy" | "offline" | "untested".
   - `probingIds`: Set of currently probing models.
   - `isProbingAll`: boolean flag showing live progress.

- [ ] **Step 2: Mount Component to `/health` Route**
Update route table in web assets so navigation to `/health` renders the Health Matrix view.

- [ ] **Step 3: Synchronize assets with `internal/webdist/`**
Copy modified web assets to `internal/webdist/dist/assets/`.

- [ ] **Step 4: Commit**
```bash
git add web/dist/assets/ internal/webdist/dist/assets/
git commit -m "feat(web): implement responsive Model Health Matrix dashboard page"
```

---

### Task 6: Verification & End-to-End Test

**Files:**
- System-wide verification

- [ ] **Step 1: Run all unit tests**
Run: `go test ./...`
Expected: 100% PASS.

- [ ] **Step 2: Build local binary**
Run:
```bash
go build -o bin/lam-router ./cmd/lamrouter/
cp --remove-destination bin/lam-router /home/ahlfs/.local/bin/lamrouter
cp --remove-destination bin/lam-router /home/ahlfs/.local/bin/lam-router
```

- [ ] **Step 3: Restart PM2 service and verify endpoints via curl**
```bash
pm2 restart lamrouter
curl -s http://127.0.0.1:9898/v1/health/models | jq .
curl -s -X POST http://127.0.0.1:9898/v1/health/models/probe -H "Content-Type: application/json" -d '{"model_id":"antigravity/gemini-2.5-pro"}' | jq .
```
Expected: Valid JSON responses, updated latency, and healthy/offline status.

- [ ] **Step 4: Verify Web Dashboard in browser**
Open `http://localhost:9898/health` and verify:
1. Navigation displays "Model Health".
2. KPI cards display counts and average latency.
3. Desktop shows table; mobile view displays stacked cards.
4. Clicking `[Test]` on a model triggers probe and updates latency in real-time.
5. All icons render as clean SVGs (no emojis).

- [ ] **Step 5: Commit & Push to GitHub**
```bash
git push origin main
```
