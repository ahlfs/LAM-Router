package srouterapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitSchema_CompressionTokensColumn(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	// First initialization: table should be created with compression_tokens column
	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Verify column exists in schema
	var compressionTokens int
	err = dbConn.QueryRow("SELECT compression_tokens FROM request_logs LIMIT 1").Scan(&compressionTokens)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("failed to query compression_tokens: %v", err)
	}

	// Re-run InitSchema on existing table to verify safe migration without error
	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed on second run: %v", err)
	}

	// Test migration when table pre-exists without compression_tokens
	legacyDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open legacy sqlite in-memory db: %v", err)
	}
	defer legacyDB.Close()

	legacySchema := `
	CREATE TABLE request_logs (
		id TEXT PRIMARY KEY,
		api_key_id TEXT,
		provider_id TEXT NOT NULL,
		model TEXT NOT NULL,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		status_code INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);
	`
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}

	// Run InitSchema on legacy DB
	if err := InitSchema(legacyDB); err != nil {
		t.Fatalf("InitSchema on legacy DB failed: %v", err)
	}

	// Verify column was added
	err = legacyDB.QueryRow("SELECT compression_tokens FROM request_logs LIMIT 1").Scan(&compressionTokens)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("failed to query migrated compression_tokens: %v", err)
	}
}

func TestHandleLogsStats_UnifiedEfficiencyMetrics(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Insert test rows with prompt_tokens, completion_tokens, cached_tokens, compression_tokens
	_, err = dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES 
('req-1', 'key-1', 'prov-1', 'gpt-4o', 1000, 200, 1200, 200, 50, 1000000, 400, 150, 0.05),
('req-2', 'key-1', 'prov-1', 'claude-3-5', 2000, 500, 2500, 200, 100, 1000010, 600, 250, 0.10)
`)
	if err != nil {
		t.Fatalf("failed to insert test request logs: %v", err)
	}

	h := &SRouterHandler{
		db: dbConn,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/logs/stats", nil)
	rr := httptest.NewRecorder()

	h.HandleLogsStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check totalCachedTokens = 400 + 600 = 1000
	cached, ok := resp["totalCachedTokens"].(float64)
	if !ok || int(cached) != 1000 {
		t.Errorf("expected totalCachedTokens=1000, got %v", resp["totalCachedTokens"])
	}

	// Check totalCompressionTokens = 150 + 250 = 400
	comp, ok := resp["totalCompressionTokens"].(float64)
	if !ok || int(comp) != 400 {
		t.Errorf("expected totalCompressionTokens=400, got %v", resp["totalCompressionTokens"])
	}

	// Check totalSavedTokens = 1000 + 400 = 1400
	saved, ok := resp["totalSavedTokens"].(float64)
	if !ok || int(saved) != 1400 {
		t.Errorf("expected totalSavedTokens=1400, got %v", resp["totalSavedTokens"])
	}

	// Check byModel metrics
	byModelRaw, ok := resp["byModel"].([]any)
	if !ok || len(byModelRaw) != 2 {
		t.Fatalf("expected byModel slice of length 2, got %v", resp["byModel"])
	}

	for _, item := range byModelRaw {
		m := item.(map[string]any)
		modelName := m["model"].(string)
		if modelName == "gpt-4o" {
			if int(m["totalCachedTokens"].(float64)) != 400 {
				t.Errorf("gpt-4o expected totalCachedTokens=400, got %v", m["totalCachedTokens"])
			}
			if int(m["totalCompressionTokens"].(float64)) != 150 {
				t.Errorf("gpt-4o expected totalCompressionTokens=150, got %v", m["totalCompressionTokens"])
			}
			if int(m["totalSavedTokens"].(float64)) != 550 {
				t.Errorf("gpt-4o expected totalSavedTokens=550, got %v", m["totalSavedTokens"])
			}
		} else if modelName == "claude-3-5" {
			if int(m["totalCachedTokens"].(float64)) != 600 {
				t.Errorf("claude-3-5 expected totalCachedTokens=600, got %v", m["totalCachedTokens"])
			}
			if int(m["totalCompressionTokens"].(float64)) != 250 {
				t.Errorf("claude-3-5 expected totalCompressionTokens=250, got %v", m["totalCompressionTokens"])
			}
			if int(m["totalSavedTokens"].(float64)) != 850 {
				t.Errorf("claude-3-5 expected totalSavedTokens=850, got %v", m["totalSavedTokens"])
			}
		}
	}
}

func TestHandleCacheGet_HybridTelemetry(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Insert test rows in request_logs
	_, err = dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES 
('req-1', 'key-1', 'prov-1', 'gpt-4o', 1000, 200, 1200, 200, 50, 1000000, 500, 300, 0.05)
`)
	if err != nil {
		t.Fatalf("failed to insert test request logs: %v", err)
	}

	h := &SRouterHandler{
		db: dbConn,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/settings/cache", nil)
	rr := httptest.NewRecorder()

	h.HandleCacheGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check backwards-compatibility fields exist
	if _, ok := resp["enabled"]; !ok {
		t.Error("missing 'enabled' field")
	}
	if _, ok := resp["ttlSeconds"]; !ok {
		t.Error("missing 'ttlSeconds' field")
	}
	if _, ok := resp["totalCached"]; !ok {
		t.Error("missing 'totalCached' field")
	}
	if _, ok := resp["tokensSaved"]; !ok {
		t.Error("missing 'tokensSaved' field")
	}

	// Check hybrid persistent fields
	upstream, ok := resp["totalUpstreamCached"].(float64)
	if !ok || int(upstream) != 500 {
		t.Errorf("expected totalUpstreamCached=500, got %v", resp["totalUpstreamCached"])
	}

	comp, ok := resp["totalCompressionSaved"].(float64)
	if !ok || int(comp) != 300 {
		t.Errorf("expected totalCompressionSaved=300, got %v", resp["totalCompressionSaved"])
	}

	saved, ok := resp["totalSavedTokens"].(float64)
	if !ok || int(saved) < 800 {
		t.Errorf("expected totalSavedTokens >= 800, got %v", resp["totalSavedTokens"])
	}
}
