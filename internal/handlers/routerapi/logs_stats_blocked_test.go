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

// A request the auth middleware rejects is stored with the synthetic provider
// id and model id "auth" (internal/middleware/auth.go), because no real
// provider or model has been resolved at that point. Grouping on the model
// column surfaced that placeholder as a model named "Auth" in the dashboard.
// This pins the fixed contract: those rows are excluded from the model and
// provider breakdowns and reported through "blocked" instead, while the card
// totals keep counting every request in the window.
func TestHandleLogsStats_RejectedRequestsAreNotAModel(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	now := time.Now()
	insert := func(id, providerID, model string, status, tokens int, created int64) {
		if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost, fallback_reason)
VALUES (?, 'key-1', ?, ?, 0, 0, ?, ?, 12, ?, 0, 0, 0, ?)
`, id, providerID, model, tokens, status, created, "reason-"+id); err != nil {
			t.Fatalf("failed to insert %s: %v", id, err)
		}
	}

	// Real usage.
	insert("req-real", "antigravity", "gemini-3.8-flash-high", 200, 120, now.UnixMilli())
	insert("req-real-old", "antigravity", "claude-sonnet-4-6", 200, 90, now.Add(-40*24*time.Hour).UnixMilli())

	// Requests rejected by the middleware: no tokens, provider names the auth
	// stage and there is no model. The placeholder pair is the shape older
	// builds wrote and has to keep being classified the same way.
	insert("req-blocked-401", "auth", "auth", 401, 0, now.Add(-time.Hour).UnixMilli())
	insert("req-blocked-429a", "auth", "auth", 429, 0, now.Add(-time.Hour).UnixMilli())
	insert("req-blocked-429b", "auth", "", 429, 0, now.Add(-30*time.Minute).UnixMilli())
	insert("req-blocked-old", "auth", "auth", 401, 0, now.Add(-40*24*time.Hour).UnixMilli())

	h := &RouterAPIHandler{db: dbConn}

	query := func(period string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/v1/logs/stats?period="+period, nil)
		rr := httptest.NewRecorder()
		h.HandleLogsStats(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("period %q: expected status 200, got %d", period, rr.Code)
		}
		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("period %q: failed to decode response: %v", period, err)
		}
		return resp
	}

	counts := func(resp map[string]any, key string) map[string]int {
		raw, ok := resp[key].([]any)
		if !ok {
			t.Fatalf("%s missing from response: %v", key, resp[key])
		}
		out := map[string]int{}
		for _, entry := range raw {
			row, ok := entry.(map[string]any)
			if !ok {
				t.Fatalf("%s row is not an object: %v", key, entry)
			}
			label := row["model"]
			if key == "byProvider" {
				label = row["providerId"]
			}
			n, _ := row["totalRequests"].(float64)
			out[label.(string)] = int(n)
		}
		return out
	}

	// All time: the placeholder must not appear as a model or as a provider.
	all := query("all")
	models := counts(all, "byModel")
	if _, leaked := models["auth"]; leaked {
		t.Fatalf("model breakdown still lists the auth placeholder: %v", models)
	}
	if _, leaked := models[""]; leaked {
		t.Fatalf("a rejected request leaked into the model breakdown as an empty model: %v", models)
	}
	if len(models) != 2 || models["gemini-3.8-flash-high"] != 1 || models["claude-sonnet-4-6"] != 1 {
		t.Fatalf("unexpected model breakdown, got %v", models)
	}
	providers := counts(all, "byProvider")
	if _, leaked := providers["auth"]; leaked {
		t.Fatalf("provider breakdown still lists the auth placeholder: %v", providers)
	}
	if len(providers) != 1 || providers["antigravity"] != 2 {
		t.Fatalf("unexpected provider breakdown, got %v", providers)
	}

	// The breakdowns answer "how much usage"; the card total answers "how much
	// traffic", so it keeps counting the rejected requests.
	if total, _ := all["totalRequests"].(float64); int(total) != 6 {
		t.Fatalf("expected the window total to keep counting all 6 rows, got %v", all["totalRequests"])
	}

	// Rejected requests are reported separately, split by status code.
	blocked, ok := all["blocked"].(map[string]any)
	if !ok {
		t.Fatalf("blocked section missing from response: %v", all["blocked"])
	}
	if n, _ := blocked["total"].(float64); int(n) != 4 {
		t.Fatalf("expected 4 blocked requests, got %v", blocked["total"])
	}
	byStatus, _ := blocked["byStatus"].(map[string]any)
	for status, want := range map[string]int{"401": 2, "429": 2} {
		got, _ := byStatus[status].(float64)
		if int(got) != want {
			t.Fatalf("status %s: expected %d blocked requests, got %v", status, want, byStatus[status])
		}
	}

	// The period window has to compose with the placeholder filter: the two
	// 40-day-old rows fall outside 24h in both sections.
	recent := query("24h")
	if total, _ := recent["totalRequests"].(float64); int(total) != 4 {
		t.Fatalf("24h: expected 4 rows (1 real + 3 rejected), got %v", recent["totalRequests"])
	}
	recentBlocked, _ := recent["blocked"].(map[string]any)
	if n, _ := recentBlocked["total"].(float64); int(n) != 3 {
		t.Fatalf("24h: expected 3 blocked requests, got %v", recentBlocked["total"])
	}
	recentStatus, _ := recentBlocked["byStatus"].(map[string]any)
	if got, _ := recentStatus["401"].(float64); int(got) != 1 {
		t.Fatalf("24h: expected 1 unauthorized request, got %v", recentStatus["401"])
	}
	if _, leaked := counts(recent, "byModel")["auth"]; leaked {
		t.Fatalf("24h model breakdown still lists the auth placeholder")
	}

}
