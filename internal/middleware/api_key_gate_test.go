package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"lamrouter/internal/db"
)

// openModeDB builds the minimum schema RequireApiKey touches plus the settings
// row that opens the gateway.
func openModeDB(t *testing.T, requireAPIKey string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	stmts := []string{
		`CREATE TABLE api_keys (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL,
			name TEXT,
			enabled INTEGER DEFAULT 1,
			rate_limit INTEGER,
			quota_limit INTEGER,
			usage_tokens INTEGER DEFAULT 0,
			credit_limit REAL,
			usage_cost REAL DEFAULT 0,
			allowed_models TEXT,
			created_at INTEGER
		)`,
		`CREATE TABLE request_logs (
			id TEXT PRIMARY KEY, api_key_id TEXT, api_key_name TEXT, provider_id TEXT,
			model TEXT, prompt_tokens INTEGER, completion_tokens INTEGER, total_tokens INTEGER,
			status_code INTEGER, latency_ms INTEGER, created_at INTEGER, cached_tokens INTEGER,
			cache_creation_tokens INTEGER, compression_tokens INTEGER, reasoning_tokens INTEGER,
			estimated_cost REAL, fallback_occurred INTEGER, fallback_reason TEXT
		)`,
		`CREATE TABLE admin_sessions (token_hash TEXT PRIMARY KEY, created_at INTEGER, expires_at INTEGER)`,
		`CREATE TABLE system_settings (key TEXT PRIMARY KEY, value TEXT)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			t.Fatalf("schema: %v (%s)", err, s)
		}
	}
	if requireAPIKey != "" {
		if _, err := conn.Exec(`INSERT INTO system_settings (key, value) VALUES ('require_api_key', ?)`, requireAPIKey); err != nil {
			t.Fatalf("seed setting: %v", err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES ('k1','lam-live-test','Test',1,0)`); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return conn
}

func serveWithKey(t *testing.T, database *sql.DB, header string) (int, *modelsKey) {
	t.Helper()
	repo := db.NewRepo(database)
	var captured *modelsKey

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if k := GetAuthenticatedApiKey(r); k != nil {
			captured = &modelsKey{ID: k.ID, Name: derefString(k.Name)}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	RequireApiKey(repo)(next).ServeHTTP(rec, req)
	return rec.Code, captured
}

type modelsKey struct {
	ID   string
	Name string
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Open access mode must actually let a keyless request through, otherwise the
// Settings toggle is a lie.
func TestRequireApiKey_OpenModeAllowsKeylessRequest(t *testing.T) {
	database := openModeDB(t, "false")
	code, key := serveWithKey(t, database, "")
	if code != http.StatusOK {
		t.Fatalf("expected open mode to allow keyless request, got %d", code)
	}
	if key != nil {
		t.Fatalf("no key was presented, so none should be attributed: %+v", key)
	}
}

func TestRequireApiKey_OpenModeStillAttributesProvidedKey(t *testing.T) {
	database := openModeDB(t, "false")
	code, key := serveWithKey(t, database, "Bearer lam-live-test")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if key == nil || key.ID != "k1" {
		t.Fatalf("a valid key should still be attributed in open mode, got %+v", key)
	}
}

func TestRequireApiKey_OpenModeRejectsInactiveKeyButAllowsThrough(t *testing.T) {
	database := openModeDB(t, "false")
	if _, err := database.Exec(`UPDATE api_keys SET enabled = 0 WHERE id = 'k1'`); err != nil {
		t.Fatal(err)
	}
	code, key := serveWithKey(t, database, "Bearer lam-live-test")
	if code != http.StatusOK {
		t.Fatalf("open mode should not reject a stale key, got %d", code)
	}
	if key != nil {
		t.Fatalf("an inactive key must not be attributed: %+v", key)
	}
}

func TestRequireApiKey_EnforcedByDefault(t *testing.T) {
	for _, setting := range []string{"", "true", "TRUE", "1"} {
		database := openModeDB(t, setting)
		code, _ := serveWithKey(t, database, "")
		if code != http.StatusUnauthorized {
			t.Fatalf("setting %q: expected 401 without a key, got %d", setting, code)
		}
	}
}

func TestRequireApiKey_EnforcedModeAcceptsValidKey(t *testing.T) {
	database := openModeDB(t, "true")
	code, key := serveWithKey(t, database, "Bearer lam-live-test")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if key == nil || key.ID != "k1" {
		t.Fatalf("expected key attribution, got %+v", key)
	}
}

var _ = context.Background
