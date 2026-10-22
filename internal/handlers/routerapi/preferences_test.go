package routerapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lamrouter/internal/gatewayprefs"
)

func TestPreferencesGet_ReturnsDefaults(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	h.HandlePreferencesGet(rec, httptest.NewRequest(http.MethodGet, "/v1/settings/preferences", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET preferences = %d", rec.Code)
	}
	var body struct {
		Preferences gatewayprefs.Preferences `json:"preferences"`
		Defaults    gatewayprefs.Preferences `json:"defaults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Preferences != gatewayprefs.Defaults() {
		t.Fatalf("preferences = %+v, want defaults", body.Preferences)
	}
}

func TestPreferencesUpdate_PartialAndValidated(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	post := func(payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/settings/preferences", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandlePreferencesUpdate(rec, req)
		return rec
	}

	if rec := post(`{"logRetentionDays":7}`); rec.Code != http.StatusOK {
		t.Fatalf("valid patch = %d: %s", rec.Code, rec.Body.String())
	}
	prefs := gatewayprefs.Load(h.db)
	if prefs.LogRetentionDays != 7 {
		t.Fatalf("window = %d, want 7", prefs.LogRetentionDays)
	}
	if prefs.LoggingLevel != gatewayprefs.Defaults().LoggingLevel {
		t.Fatalf("sibling field was clobbered: %+v", prefs)
	}

	if rec := post(`{"loggingLevel":"metadata","recordTokenUsage":false}`); rec.Code != http.StatusOK {
		t.Fatalf("valid patch = %d: %s", rec.Code, rec.Body.String())
	}
	prefs = gatewayprefs.Load(h.db)
	if prefs.LoggingLevel != gatewayprefs.LoggingLevelMetadata || prefs.RecordTokenUsage || prefs.LogRetentionDays != 7 {
		t.Fatalf("unexpected prefs after patch: %+v", prefs)
	}

	for _, bad := range []string{
		`{"loggingLevel":"verbose"}`,
		`{"logRetentionDays":0}`,
		`{"logRetentionDays":9999}`,
		`{}`,
		`{not json}`,
	} {
		if rec := post(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("payload %s should be rejected, got %d: %s", bad, rec.Code, rec.Body.String())
		}
	}
}
