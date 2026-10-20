package routerapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"lamrouter/internal/db"
	"lamrouter/internal/handlers/shared"
	_ "modernc.org/sqlite"
)

func setupTestRouterAPI(t *testing.T) (*RouterAPIHandler, *db.Repo, func()) {
	t.Helper()
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	dbConn.SetMaxOpenConns(1)

	repo := db.NewRepo(dbConn)
	repo.SetJWTSecret("test-system-secret-12345")
	ts := shared.NewTokenSaverConfig(false, false, false)
	h := NewRouterAPIHandler(repo, ts)
	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	cleanup := func() {
		dbConn.Close()
	}
	return h, repo, cleanup
}

func TestSecurity_BcryptPasswordAndAutoUpgrade(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	// 1. Seed legacy SHA-256 hash into admin_account
	legacyPassword := "my-secret-pass-123"
	legacySalt := "lam-router-salt"
	sum := sha256.Sum256([]byte(legacySalt + ":" + legacyPassword))
	legacyHash := hex.EncodeToString(sum[:])
	now := time.Now().UnixMilli()

	_, err := h.db.Exec("INSERT INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", legacyHash, now, now)
	if err != nil {
		t.Fatalf("failed to insert legacy admin account: %v", err)
	}

	// 2. Perform Login with the legacy password
	loginBody, _ := json.Marshal(map[string]string{"password": legacyPassword})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleAdminLogin(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on legacy login, got %d: %s", w.Code, w.Body.String())
	}

	// Verify cookie was set
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == AdminSessionCookie {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected admin session cookie to be set")
	}

	// 3. Verify that stored hash was automatically upgraded to bcrypt ($2a$)!
	var upgradedHash string
	_ = h.db.QueryRow("SELECT password_hash FROM admin_account WHERE id = 1").Scan(&upgradedHash)
	if !bytes.HasPrefix([]byte(upgradedHash), []byte("$2a$")) {
		t.Fatalf("expected hash to be upgraded to bcrypt ($2a$), got %s", upgradedHash)
	}

	// 4. Test login again with upgraded bcrypt hash
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewReader(loginBody))
	req2.Header.Set("Content-Type", "application/json")
	h.HandleAdminLogin(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on bcrypt login, got %d", w2.Code)
	}
}

func TestSecurity_RateLimiting(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	// Seed account
	hash, _ := HashPassword("correct-pass")
	now := time.Now().UnixMilli()
	_, _ = h.db.Exec("INSERT INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", hash, now, now)

	// Perform 5 wrong password attempts from specific IP
	testIP := "192.0.2.10:1234"
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		body, _ := json.Marshal(map[string]string{"password": "wrong-password"})
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		h.HandleAdminLogin(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	// 6th attempt should be rate limited with 429 Too Many Requests
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"password": "correct-pass"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewReader(body))
	req.RemoteAddr = testIP
	h.HandleAdminLogin(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests on 6th attempt, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSecurity_AdminRoutesProtected(t *testing.T) {
	_, repo, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	r := chi.NewRouter()
	RegisterV1Routes(r, repo, shared.NewTokenSaverConfig(false, false, false))

	// 1. Unauthenticated request to /v1/keys MUST return 401 Unauthorized
	req := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated /v1/keys, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Unauthenticated request to /v1/providers MUST return 401 Unauthorized
	req2 := httptest.NewRequest(http.MethodGet, "/v1/providers", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated /v1/providers, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Request with valid System Secret token MUST succeed
	req3 := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	req3.Header.Set("Authorization", "Bearer test-system-secret-12345")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with system secret, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestSecurity_SSRFBlockedOnBackupImport(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	// Attempt SSRF using loopback IP
	payload := map[string]string{
		"url": "http://127.0.0.1:20128/secret-endpoint",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/settings/backup/import", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleBackupImport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for SSRF attempt, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("SSRF")) {
		t.Fatalf("expected error message to mention SSRF, got: %s", w.Body.String())
	}
}
