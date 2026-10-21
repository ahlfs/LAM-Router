package middleware

import (
	"database/sql"
	"testing"

	"lamrouter/internal/db"

	_ "modernc.org/sqlite"
)

// logAuthError records a request the middleware refused before any routing
// happened. The row names the stage in provider_id, but it must not claim a
// model: an "auth" value in the model column is what made the dashboard list
// "Auth" as a model alongside the real ones.
func TestLogAuthError_RecordsRejectionWithoutAModel(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`
CREATE TABLE request_logs (
	id TEXT PRIMARY KEY,
	api_key_id TEXT,
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

	logAuthError(db.NewRepo(conn), "key-abc", "Authentication required", 401)

	var providerID, model, reason string
	var status int
	if err := conn.QueryRow(`
SELECT provider_id, model, status_code, COALESCE(fallback_reason, '') FROM request_logs
`).Scan(&providerID, &model, &status, &reason); err != nil {
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
}
