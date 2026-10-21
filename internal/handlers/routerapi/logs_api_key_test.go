package routerapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"lamrouter/internal/models"
)

func logsList(t *testing.T, h *RouterAPIHandler, query string) []map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/logs"+query, nil)
	rr := httptest.NewRecorder()
	h.HandleLogsList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("%q: expected 200, got %d", query, rr.Code)
	}

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%q: decode response: %v", query, err)
	}
	return resp.Data
}

// The dashboard shows one row per request: "which key did this" has to be
// answerable from that row, both as a label (name) and as a stable handle (id).
func TestHandleLogsList_ReportsClientKeyIdentity(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dbConn.Close()

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	now := time.Now().UnixMilli()
	if _, err := dbConn.Exec(`
INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES
	('key_ops', 'lam-live-ops-secret', 'Ops Team', 1, ?),
	('key_dev', 'lam-live-dev-secret', 'Dev Team', 1, ?)
`, now, now); err != nil {
		t.Fatalf("seed api keys: %v", err)
	}

	if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, api_key_name, provider_id, model, status_code, latency_ms, created_at) VALUES
	('req-ops', 'key_ops', 'Ops Team', 'openai', 'gpt-4o', 200, 12, ?),
	('req-dev', 'key_dev', 'Dev Team', 'openai', 'gpt-4o-mini', 200, 20, ?),
	('req-refused', 'unmatched:1a2b3c4d', '', 'auth', '', 401, 0, ?)
`, now, now, now); err != nil {
		t.Fatalf("seed request logs: %v", err)
	}

	h := &RouterAPIHandler{db: dbConn}

	byID := map[string]map[string]any{}
	for _, row := range logsList(t, h, "") {
		byID[row["id"].(string)] = row
	}
	if len(byID) != 3 {
		t.Fatalf("expected 3 log rows, got %d", len(byID))
	}
	if got := byID["req-ops"]["apiKeyName"]; got != "Ops Team" {
		t.Errorf("expected the key name to be served, got %v", got)
	}
	if got := byID["req-ops"]["apiKeyId"]; got != "key_ops" {
		t.Errorf("expected the key id to be served, got %v", got)
	}
	if got := byID["req-refused"]["apiKeyName"]; got != models.UnknownClientKeyName {
		t.Errorf("a refusal of an unknown key should carry the unknown-key label, got %v", got)
	}

	filtered := logsList(t, h, "?apiKeyId=key_ops")
	if len(filtered) != 1 || filtered[0]["id"] != "req-ops" {
		t.Fatalf("apiKeyId filter must narrow to that key's requests, got %v", filtered)
	}

	// Unknown ids must not silently return every row.
	if got := logsList(t, h, "?apiKeyId=key_missing"); len(got) != 0 {
		t.Fatalf("expected no rows for an unknown key, got %v", got)
	}
}

// Databases written by older builds stored the raw client key in api_key_id (and
// the masked upstream key when the caller had none). Opening one has to leave an
// audit trail that identifies keys without keeping a usable credential.
func TestInitSchema_MigratesLegacyClientKeys(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dbConn.Close()

	if _, err := dbConn.Exec(`
CREATE TABLE api_keys (
	id TEXT PRIMARY KEY,
	key TEXT UNIQUE NOT NULL,
	name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	rate_limit INTEGER DEFAULT 0,
	quota_limit INTEGER DEFAULT 0,
	usage_tokens INTEGER DEFAULT 0,
	credit_limit REAL DEFAULT 0,
	usage_cost REAL DEFAULT 0,
	allowed_models TEXT,
	created_at INTEGER NOT NULL
);
CREATE TABLE request_logs (
	id TEXT PRIMARY KEY,
	api_key_id TEXT,
	provider_id TEXT NOT NULL,
	model TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	status_code INTEGER DEFAULT 0,
	latency_ms INTEGER DEFAULT 0,
	created_at INTEGER NOT NULL,
	cached_tokens INTEGER DEFAULT 0,
	cache_creation_tokens INTEGER DEFAULT 0,
	compression_tokens INTEGER DEFAULT 0,
	reasoning_tokens INTEGER DEFAULT 0,
	estimated_cost REAL DEFAULT 0,
	fallback_occurred INTEGER DEFAULT 0,
	fallback_reason TEXT
);
INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES ('key_ops', 'lam-live-ops-secret', 'Ops Team', 1, 1);
INSERT INTO request_logs (id, api_key_id, provider_id, model, created_at) VALUES
	('req-old-raw', 'lam-live-ops-secret', 'openai', 'gpt-4o', 1000),
	('req-old-deleted', 'lam-live-deleted-secret', 'openai', 'gpt-4o', 1001),
	('req-old-none', 'none', 'auth', '', 1002);
`); err != nil {
		t.Fatalf("build legacy schema: %v", err)
	}

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	read := func(id string) (string, string) {
		t.Helper()
		var keyID, keyName string
		if err := dbConn.QueryRow(`SELECT api_key_id, COALESCE(api_key_name, '') FROM request_logs WHERE id = ?`, id).Scan(&keyID, &keyName); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return keyID, keyName
	}

	if keyID, keyName := read("req-old-raw"); keyID != "key_ops" || keyName != "Ops Team" {
		t.Errorf("a known raw key must migrate to id+name, got id=%q name=%q", keyID, keyName)
	}
	if keyID, _ := read("req-old-deleted"); keyID == "lam-live-deleted-secret" || !strings.Contains(keyID, "…") {
		t.Errorf("a deleted key's credential must be masked, got %q", keyID)
	}
	if keyID, _ := read("req-old-none"); keyID != "none" {
		t.Errorf("a keyless row must stay keyless, got %q", keyID)
	}

	var leaked int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE api_key_id LIKE '%secret%'`).Scan(&leaked); err != nil {
		t.Fatalf("scan for leaked keys: %v", err)
	}
	if leaked != 0 {
		t.Errorf("expected no raw credential left in request_logs, found %d rows", leaked)
	}
}
