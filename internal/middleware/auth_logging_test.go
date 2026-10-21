package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lamrouter/internal/db"

	_ "modernc.org/sqlite"
)

// authLogDB builds the two tables the middleware touches: the audit log it
// writes and the api_keys table it authenticates against.
func authLogDB(t *testing.T) *sql.DB {
	t.Helper()

	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if _, err := conn.Exec(`
CREATE TABLE request_logs (
	id TEXT PRIMARY KEY,
	api_key_id TEXT,
	api_key_name TEXT,
	provider_id TEXT,
	model TEXT,
	prompt_tokens INTEGER,
	completion_tokens INTEGER,
	total_tokens INTEGER,
	status_code INTEGER,
	latency_ms INTEGER,
	created_at INTEGER,
	cached_tokens INTEGER,
	cache_creation_tokens INTEGER,
	compression_tokens INTEGER,
	reasoning_tokens INTEGER,
	estimated_cost REAL,
	fallback_occurred INTEGER,
	fallback_reason TEXT
)`); err != nil {
		t.Fatalf("create request_logs: %v", err)
	}

	if _, err := conn.Exec(`
CREATE TABLE api_keys (
	id TEXT PRIMARY KEY,
	key TEXT NOT NULL,
	name TEXT,
	enabled INTEGER DEFAULT 1,
	rate_limit INTEGER,
	quota_limit INTEGER,
	usage_tokens INTEGER DEFAULT 0,
	credit_limit REAL,
	usage_cost REAL DEFAULT 0,
	created_at INTEGER NOT NULL
)`); err != nil {
		t.Fatalf("create api_keys: %v", err)
	}

	if _, err := conn.Exec(`
CREATE TABLE apiKeys (
	id TEXT PRIMARY KEY,
	key TEXT NOT NULL,
	name TEXT,
	isActive INTEGER DEFAULT 1,
	createdAt TEXT
)`); err != nil {
		t.Fatalf("create legacy apiKeys: %v", err)
	}

	return conn
}

// dumpRows prints the audit log on failure paths: a missing row is usually a
// different refusal branch than the one under test.
func dumpRows(t *testing.T, conn *sql.DB) {
	t.Helper()
	rows, err := conn.Query(`SELECT api_key_id, COALESCE(api_key_name,''), COALESCE(fallback_reason,'') FROM request_logs`)
	if err != nil {
		t.Logf("dump request_logs: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, reason string
		if err := rows.Scan(&id, &name, &reason); err != nil {
			t.Logf("dump scan: %v", err)
			continue
		}
		t.Logf("row: id=%q name=%q reason=%q", id, name, reason)
	}
}

// logAuthError records a request the middleware refused before any routing
// happened. The row names the stage in provider_id, but it must not claim a
// model: an "auth" value in the model column is what made the dashboard list
// "Auth" as a model alongside the real ones.
func TestLogAuthError_RecordsRejectionWithoutAModel(t *testing.T) {
	conn := authLogDB(t)

	logAuthError(db.NewRepo(conn), "key-abc", "Tim Frontend", "Authentication required", 401)

	var providerID, model, reason, apiKeyID, apiKeyName string
	var status int
	if err := conn.QueryRow(`
SELECT provider_id, model, status_code, COALESCE(fallback_reason, ''), api_key_id, COALESCE(api_key_name, '')
FROM request_logs
`).Scan(&providerID, &model, &status, &reason, &apiKeyID, &apiKeyName); err != nil {
		t.Fatalf("read back the recorded row: %v", err)
	}

	if providerID != "auth" {
		t.Fatalf("expected provider_id %q, got %q", "auth", providerID)
	}
	if model != "" {
		t.Fatalf("a rejected request must not record a model, got %q", model)
	}
	if status != 401 {
		t.Fatalf("expected status 401, got %d", status)
	}
	if reason != "Authentication required" {
		t.Fatalf("expected the refusal reason to be kept, got %q", reason)
	}
	if apiKeyID != "key-abc" || apiKeyName != "Tim Frontend" {
		t.Fatalf("expected the key identity to be kept, got id=%q name=%q", apiKeyID, apiKeyName)
	}
}

// The audit row has to say which key was used, and must never contain the
// credential itself: a log is read by humans and exported to bug reports.
func TestRequireApiKey_LogsKeyIdentityNotTheCredential(t *testing.T) {
	conn := authLogDB(t)
	repo := db.NewRepo(conn)

	const (
		inactiveKey = "lam-live-inactive-0123456789"
		unknownKey  = "lam-live-unknown-9876543210"
	)
	if _, err := conn.Exec(`
INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES ('key_paused', ?, 'Tim Paused', 0, 1)
`, inactiveKey); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireApiKey(repo)(next)

	call := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := call(inactiveKey); code != http.StatusUnauthorized {
		t.Fatalf("inactive key: expected 401, got %d", code)
	}
	if code := call(unknownKey); code != http.StatusUnauthorized {
		t.Fatalf("unknown key: expected 401, got %d", code)
	}

	var id, name, reason string
	if err := conn.QueryRow(`
SELECT api_key_id, COALESCE(api_key_name, ''), COALESCE(fallback_reason, '')
FROM request_logs WHERE fallback_reason = 'Invalid or inactive API key'
`).Scan(&id, &name, &reason); err != nil {
		dumpRows(t, conn)
		t.Fatalf("read inactive-key row: %v", err)
	}
	if id != "key_paused" {
		t.Errorf("inactive key must be logged by id, got %q", id)
	}
	if name != "Tim Paused" {
		t.Errorf("inactive key must keep its display name, got %q", name)
	}

	if err := conn.QueryRow(`
SELECT api_key_id FROM request_logs WHERE fallback_reason = 'Invalid API key'
`).Scan(&id); err != nil {
		t.Fatalf("read unknown-key row: %v", err)
	}
	if !strings.HasPrefix(id, "unmatched:") {
		t.Errorf("unknown key must be logged as a fingerprint, got %q", id)
	}
	if strings.Contains(id, unknownKey) {
		t.Errorf("the credential must never reach the log, got %q", id)
	}

	var leaked int
	if err := conn.QueryRow(`
SELECT COUNT(*) FROM request_logs WHERE api_key_id LIKE '%lam-live-%'
`).Scan(&leaked); err != nil {
		t.Fatalf("scan for leaked keys: %v", err)
	}
	if leaked != 0 {
		t.Errorf("expected no raw credential in request_logs, found %d rows", leaked)
	}
}

// Same key in, same tag out — that is the point of the fingerprint — but the
// tag says nothing about the key it came from.
func TestUnknownKeyFingerprint(t *testing.T) {
	const key = "lam-live-secret-value"

	first := unknownKeyFingerprint(key)
	if first != unknownKeyFingerprint(key) {
		t.Fatal("fingerprint must be stable for the same key")
	}
	if first == unknownKeyFingerprint(key+"x") {
		t.Fatal("different keys must not collide on the same fingerprint")
	}
	if strings.Contains(first, key) || !strings.HasPrefix(first, "unmatched:") {
		t.Fatalf("fingerprint must not reveal the key, got %q", first)
	}
	if unknownKeyFingerprint("") != "none" {
		t.Fatal("a request with no key must be logged as none")
	}
}
