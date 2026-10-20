package routerapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The dashboard cards poll /v1/logs/stats every second, so a time range cannot
// be applied by rewriting values in the DOM — the next poll would overwrite it.
// The range therefore has to be resolved server side; this test pins that
// contract, including the cases that keep older callers working.
func TestHandleLogsStats_PeriodFilter(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()

	seed := []struct {
		id      string
		model   string
		created int64
	}{
		{"req-now", "model-recent", now.UnixMilli()},
		{"req-since-midnight", "model-recent", startOfDay},
		{"req-3d", "model-week", now.Add(-3 * 24 * time.Hour).UnixMilli()},
		{"req-20d", "model-month", now.Add(-20 * 24 * time.Hour).UnixMilli()},
		{"req-45d", "model-month", now.Add(-45 * 24 * time.Hour).UnixMilli()},
		{"req-100d", "model-old", now.Add(-100 * 24 * time.Hour).UnixMilli()},
	}

	for _, row := range seed {
		if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES (?, 'key-1', 'prov-1', ?, 100, 50, 150, 200, 25, ?, 0, 0, 0.01)
`, row.id, row.model, row.created); err != nil {
			t.Fatalf("failed to insert %s: %v", row.id, err)
		}
	}

	h := &RouterAPIHandler{db: dbConn}

	// period -> expected request count, with byModel cardinality alongside so
	// the per-model breakdown is proven to follow the window too.
	cases := []struct {
		query            string
		wantRequests     int
		wantModelsInList int
	}{
		{"", 6, 4},         // no period: all time, backwards compatible
		{"?period=all", 6, 4},
		{"?period=today", 2, 1},
		{"?period=24h", 2, 1},
		{"?period=7d", 3, 2},
		{"?period=30d", 4, 3},
		{"?period=60d", 5, 3},
		{"?period=bogus", 6, 4}, // unknown periods degrade instead of erroring
		{"?range=7d", 3, 2},     // alias accepted
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/v1/logs/stats"+tc.query, nil)
		rr := httptest.NewRecorder()
		h.HandleLogsStats(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("%q: expected status 200, got %d", tc.query, rr.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("%q: failed to decode response: %v", tc.query, err)
		}

		gotRequests, ok := resp["totalRequests"].(float64)
		if !ok || int(gotRequests) != tc.wantRequests {
			t.Errorf("%q: expected totalRequests=%d, got %v", tc.query, tc.wantRequests, resp["totalRequests"])
		}

		byModel, ok := resp["byModel"].([]any)
		if !ok {
			t.Fatalf("%q: byModel missing from response: %v", tc.query, resp["byModel"])
		}
		if len(byModel) != tc.wantModelsInList {
			t.Errorf("%q: expected %d models in byModel, got %d", tc.query, tc.wantModelsInList, len(byModel))
		}

		if echoed, _ := resp["period"].(string); echoed == "" {
			t.Errorf("%q: response must echo the resolved period", tc.query)
		}
	}
}

// "Today" has to mean local midnight, not "the last 24 hours": a request placed
// just before midnight belongs to yesterday even though it is only hours old.
func TestHandleLogsStats_TodayStartsAtLocalMidnight(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	now := time.Now()
	dayMs := int64(24 * 60 * 60 * 1000)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()

	// Noon yesterday: well within 24 hours, but not today.
	beforeMidnight := startOfDay - dayMs/2

	_, err = dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES ('req-yesterday-noon', 'key-1', 'prov-1', 'gpt-4o', 10, 10, 20, 200, 25, ?, 0, 0, 0.0)
`, beforeMidnight)
	if err != nil {
		t.Fatalf("failed to insert pre-midnight row: %v", err)
	}

	h := &RouterAPIHandler{db: dbConn}

	query := func(period string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/v1/logs/stats?period="+period, nil)
		rr := httptest.NewRecorder()
		h.HandleLogsStats(rr, req)

		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("period %q: failed to decode response: %v", period, err)
		}
		return resp
	}

	today := query("today")
	if got := int(today["totalRequests"].(float64)); got != 0 {
		t.Errorf("today must exclude the pre-midnight row, got %d requests", got)
	}

	last24h := query("24h")
	if got := int(last24h["totalRequests"].(float64)); got != 1 {
		t.Errorf("24h must include the pre-midnight row exactly once, got %d requests", got)
	}

	all := query("all")
	if got := int(all["totalRequests"].(float64)); got != 1 {
		t.Errorf("all must include every row, got %d requests", got)
	}
}
