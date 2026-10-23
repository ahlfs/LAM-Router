package routerapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// "Usage by model" renders one table row per model, so the breakdown must not
// be capped: the query used to return only the top 20 by request count, which
// silently dropped rows (and their request counts) as soon as a period had more
// models than that. Tokens could still look complete because the tail is tiny,
// which is exactly what makes the bug easy to miss.
func TestHandleLogsStats_ByModelHasNoRowCap(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	const modelCount = 35
	now := time.Now().UnixMilli()
	for i := 0; i < modelCount; i++ {
		if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES (?, 'key-1', 'prov-1', ?, 10, 5, 15, 200, 20, ?, 0, 0, 0.001)
`, fmt.Sprintf("req-%02d", i), fmt.Sprintf("model-%02d", i), now); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	h := &RouterAPIHandler{db: dbConn}
	rec := httptest.NewRecorder()
	h.HandleLogsStats(rec, httptest.NewRequest(http.MethodGet, "/v1/logs/stats?period=all", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	models, _ := resp["byModel"].([]any)
	if len(models) != modelCount {
		t.Errorf("expected %d models in byModel, got %d", modelCount, len(models))
	}

	// The per-model request counts must add up to the card total, otherwise the
	// table contradicts the header.
	sum := 0.0
	for _, m := range models {
		row, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("unexpected byModel row type %T", m)
		}
		reqs, _ := row["totalRequests"].(float64)
		sum += reqs
	}
	if int(sum) != modelCount {
		t.Errorf("byModel requests sum to %v, want %d", sum, modelCount)
	}
}
