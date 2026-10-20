package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"lamrouter/internal/config"
	"lamrouter/internal/db"
	"lamrouter/internal/handlers"
)

// buildProductionRouter mirrors the real main() wiring so route-shadowing
// regressions (e.g. an admin route leaking out of the protected group because
// of a later r.Route("/v1", ...) mount) are caught by tests.
func buildProductionRouter(t *testing.T) http.Handler {
	t.Helper()

	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	repo := db.NewRepo(dbConn)
	repo.SetJWTSecret("test-system-secret-12345")
	ts := handlers.NewTokenSaverConfig(false, false, false)

	return setupRouter(&config.Config{}, repo, ts)
}

func assertDenied(t *testing.T, r http.Handler, method, path string) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: %s %s reachable unauthenticated -> got %d, want 401/403. body=%.300s",
			method, path, w.Code, w.Body.String())
	}
}

// TestProductionRouter_KeysRequiresAdminAuth is the regression guard for the
// /v1/keys information disclosure: unauthenticated callers must never receive
// the plaintext API key list.
func TestProductionRouter_KeysRequiresAdminAuth(t *testing.T) {
	r := buildProductionRouter(t)

	t.Run("no auth at all", func(t *testing.T) {
		assertDenied(t, r, http.MethodGet, "/v1/keys")
	})

	t.Run("arbitrary bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Fatalf("SECURITY: /v1/keys reachable with a junk bearer token -> got %d, want 401/403. body=%.300s",
				w.Code, w.Body.String())
		}
	})
}

// TestProductionRouter_AdminSurfaceRequiresAuth sweeps every admin endpoint that
// was verified reachable without credentials on the public deployment, so a
// single leaked route cannot slip through unnoticed again.
func TestProductionRouter_AdminSurfaceRequiresAuth(t *testing.T) {
	r := buildProductionRouter(t)

	getPaths := []string{
		"/v1/keys",
		"/v1/providers",
		"/v1/providers/catalog",
		"/v1/logs",
		"/v1/logs/stats",
		"/v1/quota",
		"/v1/settings",
		"/v1/settings/fallbacks",
		"/v1/settings/token-saver",
		"/v1/telemetry/active",
		"/v1/health/models",
		"/v1/tunnel/status",
		"/v1/tunnel/config",
	}

	for _, p := range getPaths {
		t.Run("GET "+p, func(t *testing.T) {
			assertDenied(t, r, http.MethodGet, p)
		})
	}

	writeProbes := []struct{ method, path string }{
		{http.MethodPost, "/v1/keys"},
		{http.MethodDelete, "/v1/keys/some-key-id"},
		{http.MethodPost, "/v1/settings/cache"},
		{http.MethodPost, "/v1/providers/verify"},
		{http.MethodPost, "/v1/settings/backup/export"},
	}

	for _, wp := range writeProbes {
		t.Run(wp.method+" "+wp.path, func(t *testing.T) {
			assertDenied(t, r, wp.method, wp.path)
		})
	}
}
