package routerapi

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The provider distribution card and the provider label above the model traffic
// chart both read /v1/logs/stats. Request logs store the bare model name — the
// provider lives in its own column — which is why the dashboard used to fall
// back to a hardcoded "gateway" label with a "GA" initials badge. This pins the
// contract that fixes it: per-provider totals plus a provider on every model row.
func TestHandleLogsStats_ProviderBreakdown(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	now := time.Now()
	seed := []struct {
		id         string
		providerID string
		model      string
		tokens     int
		created    int64
	}{
		// antigravity serves the same model on two different days, so the model
		// row has a dominant provider to pick.
		{"req-a-today", "antigravity", "gemini-3.8-flash-high", 150, now.UnixMilli()},
		{"req-a-3d", "antigravity", "gemini-3.8-flash-high", 150, now.Add(-3 * 24 * time.Hour).UnixMilli()},
		{"req-a-20d", "antigravity", "claude-sonnet-4-6", 150, now.Add(-20 * 24 * time.Hour).UnixMilli()},
		// A second provider that shares the top model but carries no tokens,
		// mirroring the auth/validation rows the real database records.
		{"req-b-today", "my_custom_prov", "gemini-3.8-flash-high", 0, now.UnixMilli()},
	}

	for _, row := range seed {
		if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost)
VALUES (?, 'key-1', ?, ?, ?, 0, ?, 200, 25, ?, 0, 0, 0.01)
`, row.id, row.providerID, row.model, row.tokens, row.tokens, row.created); err != nil {
			t.Fatalf("failed to insert %s: %v", row.id, err)
		}
	}

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

	providersOf := func(resp map[string]any) []map[string]any {
		raw, ok := resp["byProvider"].([]any)
		if !ok {
			t.Fatalf("byProvider missing from response: %v", resp["byProvider"])
		}
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			entry, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("byProvider entry is not an object: %v", item)
			}
			out = append(out, entry)
		}
		return out
	}

	all := query("all")
	providers := providersOf(all)
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers in byProvider, got %d: %v", len(providers), providers)
	}

	// Sorted by token volume: the tokenless provider comes last.
	top := providers[0]
	if got := top["providerId"].(string); got != "antigravity" {
		t.Errorf("expected antigravity first, got %q", got)
	}
	if got := top["name"].(string); got != "Google Antigravity" {
		t.Errorf("catalog name lookup failed: got %q, want %q", got, "Google Antigravity")
	}
	if got := top["totalTokens"].(float64); got != 450 {
		t.Errorf("expected 450 tokens for antigravity, got %v", got)
	}
	if got := top["totalRequests"].(float64); got != 3 {
		t.Errorf("expected 3 requests for antigravity, got %v", got)
	}
	if got := top["modelCount"].(float64); got != 2 {
		t.Errorf("expected 2 distinct models for antigravity, got %v", got)
	}
	if got := top["share"].(float64); math.Abs(got-100) > 0.001 {
		t.Errorf("expected the only token-carrying provider to hold 100%% share, got %v", got)
	}

	// Providers absent from the catalog still need a readable label.
	second := providers[1]
	if got := second["providerId"].(string); got != "my_custom_prov" {
		t.Errorf("expected my_custom_prov second, got %q", got)
	}
	if got := second["name"].(string); got != "My Custom Prov" {
		t.Errorf("fallback name formatting failed: got %q, want %q", got, "My Custom Prov")
	}
	if got := second["share"].(float64); got != 0 {
		t.Errorf("a provider without tokens must report 0 share, got %v", got)
	}

	shares := 0.0
	for _, p := range providers {
		shares += p["share"].(float64)
	}
	if math.Abs(shares-100) > 0.001 {
		t.Errorf("provider shares should add up to 100%%, got %v", shares)
	}

	// Every model row needs its provider so the chart can show the real name and
	// icon instead of the "gateway" placeholder.
	byModel, ok := all["byModel"].([]any)
	if !ok || len(byModel) != 2 {
		t.Fatalf("expected 2 models in byModel, got %v", all["byModel"])
	}
	providerOfModel := map[string]string{}
	for _, item := range byModel {
		row := item.(map[string]any)
		providerID, _ := row["providerId"].(string)
		if providerID == "" {
			t.Errorf("model %v has no providerId", row["model"])
		}
		providerOfModel[row["model"].(string)] = providerID
	}
	if got := providerOfModel["gemini-3.8-flash-high"]; got != "antigravity" {
		t.Errorf("the model shared by two providers should report the dominant one, got %q", got)
	}

	// The distribution must talk about the same window as the cards, otherwise
	// the percentages would describe a different slice of traffic than the totals.
	today := query("today")
	todayProviders := providersOf(today)
	if len(todayProviders) != 2 {
		t.Fatalf("expected both providers to have traffic today, got %d", len(todayProviders))
	}
	if got := todayProviders[0]["totalTokens"].(float64); got != 150 {
		t.Errorf("today: expected 150 tokens for antigravity, got %v", got)
	}
	if got := todayProviders[0]["totalRequests"].(float64); got != 1 {
		t.Errorf("today: expected 1 request for antigravity, got %v", got)
	}
	// Only today's row carries the shared model, so the model breakdown shrinks
	// with the window as well.
	if got := len(today["byModel"].([]any)); got != 1 {
		t.Errorf("today: expected 1 model in byModel, got %d", got)
	}
}

// The card renders on every poll, including on a fresh install where no request
// has been logged yet: an empty window must answer with an empty list rather
// than null, and the share maths must not divide by zero.
func TestHandleLogsStats_ProviderBreakdownEmpty(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	h := &RouterAPIHandler{db: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/v1/logs/stats?period=all", nil)
	rr := httptest.NewRecorder()
	h.HandleLogsStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	raw, ok := resp["byProvider"].([]any)
	if !ok {
		t.Fatalf("byProvider must decode as an empty list, got %#v", resp["byProvider"])
	}
	if len(raw) != 0 {
		t.Errorf("expected no providers without traffic, got %v", raw)
	}
	if got := resp["totalTokens"].(float64); got != 0 {
		t.Errorf("expected 0 total tokens, got %v", got)
	}
}
