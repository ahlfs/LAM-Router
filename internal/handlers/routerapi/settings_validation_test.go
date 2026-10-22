package routerapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lamrouter/internal/updater"
)

// ---------------------------------------------------------------- password

func TestAdminChangePassword_RejectsShortPassword(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	hash, err := HashPassword("oldpass123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := h.db.Exec("INSERT INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, 0, 0)", hash); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	loginBody, _ := json.Marshal(map[string]string{"password": "oldpass123"})
	loginRec := httptest.NewRecorder()
	h.HandleAdminLogin(loginRec, httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewReader(loginBody)))
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == AdminSessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no session cookie issued")
	}

	change := func(newPass string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]string{
			"currentPassword": "oldpass123",
			"newPassword":     newPass,
			"confirmation":    newPass,
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/change-password", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.HandleAdminChangePassword(rec, req)
		return rec
	}

	short := change("1")
	if short.Code != http.StatusBadRequest {
		t.Fatalf("a one-character password must be rejected, got %d: %s", short.Code, short.Body.String())
	}
	if !strings.Contains(short.Body.String(), "at least 8 characters") {
		t.Fatalf("unexpected error message: %s", short.Body.String())
	}

	var stored string
	_ = h.db.QueryRow("SELECT password_hash FROM admin_account WHERE id = 1").Scan(&stored)
	if stored != hash {
		t.Fatalf("password hash changed despite a rejected request")
	}

	if ok := change("longenough1"); ok.Code != http.StatusOK {
		t.Fatalf("a compliant password should be accepted, got %d: %s", ok.Code, ok.Body.String())
	}
}

// ------------------------------------------------------------------ proxy

func TestSettingsUpdate_ValidatesProxyURL(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	post := func(value string) (*httptest.ResponseRecorder, map[string]any) {
		payload, _ := json.Marshal(map[string]any{"outboundProxy": value})
		req := httptest.NewRequest(http.MethodPost, "/v1/settings", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleSettingsUpdate(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}

	for _, bad := range []string{"bukan url sama sekali", "ftp://proxy.local:21", "http://", "://tanpa-skema"} {
		rec, _ := post(bad)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q should be rejected, got %d: %s", bad, rec.Code, rec.Body.String())
		}
	}

	rec, body := post("http://127.0.0.1:3128")
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid http proxy must be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	if body["outboundProxy"] != "http://127.0.0.1:3128" {
		t.Fatalf("proxy not stored: %v", body["outboundProxy"])
	}

	rec, body = post("")
	if rec.Code != http.StatusOK || body["outboundProxy"] != "" {
		t.Fatalf("empty string must clear the proxy, got %d %v", rec.Code, body["outboundProxy"])
	}
}

// ----------------------------------------------------------------- version

func TestSettingsGet_ReportsBinaryVersion(t *testing.T) {
	previous := updater.CurrentVersion
	updater.CurrentVersion = "9.9.9-test"
	t.Cleanup(func() { updater.CurrentVersion = previous })

	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	h.HandleSettingsGet(rec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["version"] != "9.9.9-test" {
		t.Fatalf("version = %v, want the running binary version", body["version"])
	}
}

// ------------------------------------------------------------------- cache

func TestCacheUpdate_RejectsNonPositiveTTL(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	post := func(ttl int) int {
		payload, _ := json.Marshal(map[string]any{"ttlSeconds": ttl})
		req := httptest.NewRequest(http.MethodPost, "/v1/settings/cache", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleCacheUpdate(rec, req)
		return rec.Code
	}

	for _, ttl := range []int{-5, 0} {
		if code := post(ttl); code != http.StatusBadRequest {
			t.Errorf("ttl %d should be rejected, got %d", ttl, code)
		}
	}
	if code := post(120); code != http.StatusOK {
		t.Fatalf("ttl 120 should be accepted, got %d", code)
	}
}

// ---------------------------------------------------------------- import

func TestBackupImport_RejectsForeignPayload(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	post := func(payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/settings/backup/import", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleBackupImport(rec, req)
		return rec
	}

	foreign := post(`{"bukan":"format backup"}`)
	if foreign.Code != http.StatusBadRequest {
		t.Fatalf("foreign payload must 400, got %d: %s", foreign.Code, foreign.Body.String())
	}
	if !strings.Contains(foreign.Body.String(), "Unrecognized backup payload") {
		t.Fatalf("unexpected message: %s", foreign.Body.String())
	}

	recognized := post(`{"providers":[]}`)
	if recognized.Code != http.StatusOK {
		t.Fatalf("recognized payload should be accepted, got %d: %s", recognized.Code, recognized.Body.String())
	}

	malformed := post(`{invalid json`)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON must 400, got %d", malformed.Code)
	}
}
