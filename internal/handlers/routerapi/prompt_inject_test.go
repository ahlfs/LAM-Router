package routerapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newPromptInjectTestHandler(t *testing.T) (*RouterAPIHandler, *sql.DB) {
	t.Helper()
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := InitSchema(dbConn); err != nil {
		dbConn.Close()
		t.Fatalf("InitSchema: %v", err)
	}
	if _, err := dbConn.Exec(`CREATE TABLE IF NOT EXISTS fallback_rules (
		id TEXT PRIMARY KEY,
		source_model TEXT NOT NULL,
		target_model TEXT NOT NULL,
		priority INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		trigger_on_status TEXT,
		max_retries INTEGER DEFAULT 0,
		created_at TEXT NOT NULL,
		system_prompt TEXT DEFAULT '',
		inject_mode TEXT DEFAULT 'prepend'
	)`); err != nil {
		dbConn.Close()
		t.Fatalf("create fallback_rules: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })
	return &RouterAPIHandler{db: dbConn}, dbConn
}

func postJSON(t *testing.T, h http.HandlerFunc, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func decodeEntries(t *testing.T, h *RouterAPIHandler) map[string]promptInjectEntry {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandlePromptInjectGet(rec, httptest.NewRequest(http.MethodGet, "/v1/prompt-inject", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET prompt-inject = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []promptInjectEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	out := map[string]promptInjectEntry{}
	for _, e := range resp.Entries {
		out[e.Scope+":"+e.ComboID] = e
	}
	return out
}

func seedFallbackPrompt(t *testing.T, db *sql.DB, source, prompt string, enabled any) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO fallback_rules (id, source_model, target_model, priority, enabled, trigger_on_status, max_retries, created_at, system_prompt, inject_mode)
		 VALUES ('fr-'+?, ?, 'openai/gpt-4o-mini', 1, 1, '429', 2, '2026-01-01T00:00:00Z', ?, 'prepend')`,
		source, source, prompt,
	); err != nil {
		t.Fatalf("seed fallback: %v", err)
	}
	if enabled != nil {
		if _, err := db.Exec(`UPDATE fallback_rules SET prompt_injection_enabled = ? WHERE source_model = ?`, enabled, source); err != nil {
			t.Fatalf("seed flag: %v", err)
		}
	}
}

// Legacy rows (no flag) must keep injecting, and the UI must be able to tell
// that apart from an explicit off.
func TestPromptInjectGet_LegacyRowReadsActive(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	seedFallbackPrompt(t, db, "legacy", "Be kind.", nil)

	entry := decodeEntries(t, h)["combo:legacy"]
	if !entry.Enabled {
		t.Fatalf("legacy prompt should read as enabled")
	}
	if entry.EnabledExplicit {
		t.Fatalf("legacy row must not claim an explicit flag")
	}
}

func TestPromptInjectSet_PartialUpdateKeepsSiblings(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	seedFallbackPrompt(t, db, "support", "You are {{display_name}}.", 1)
	if _, err := db.Exec(`UPDATE fallback_rules SET display_identity = 'Support Assistant' WHERE source_model = 'support'`); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", `{"scope":"combo","comboId":"support","injectMode":"append"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set = %d: %s", rec.Code, rec.Body.String())
	}

	entry := decodeEntries(t, h)["combo:support"]
	if entry.SystemPrompt != "You are {{display_name}}." {
		t.Errorf("prompt was cleared by a mode-only update: %q", entry.SystemPrompt)
	}
	if entry.DisplayIdentity != "Support Assistant" {
		t.Errorf("identity was cleared by a mode-only update: %q", entry.DisplayIdentity)
	}
	if !entry.Enabled {
		t.Errorf("enabled flag was cleared by a mode-only update")
	}
	if entry.InjectMode != "append" {
		t.Errorf("mode = %q, want append", entry.InjectMode)
	}
}

func TestPromptInjectSet_RejectsStringBoolean(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	seedFallbackPrompt(t, db, "support", "hi", nil)

	rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", `{"scope":"combo","comboId":"support","enabled":"true"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("string boolean must be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be a boolean") {
		t.Fatalf("unexpected error message: %s", rec.Body.String())
	}
}

func TestPromptInjectSet_TogglesFlag(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	seedFallbackPrompt(t, db, "support", "Be kind.", nil)

	rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", `{"scope":"combo","comboId":"support","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d: %s", rec.Code, rec.Body.String())
	}
	entry := decodeEntries(t, h)["combo:support"]
	if entry.Enabled || !entry.EnabledExplicit {
		t.Fatalf("expected explicit off, got enabled=%v explicit=%v", entry.Enabled, entry.EnabledExplicit)
	}
	if entry.SystemPrompt != "Be kind." {
		t.Fatalf("turning the switch off must keep the stored prompt: %q", entry.SystemPrompt)
	}

	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", `{"scope":"combo","comboId":"support","enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", rec.Code, rec.Body.String())
	}
	if entry := decodeEntries(t, h)["combo:support"]; !entry.Enabled {
		t.Fatalf("expected explicit on")
	}
}

func TestPromptInjectSet_NullClearsPromptAndIdentity(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	seedFallbackPrompt(t, db, "support", "Be kind.", 1)
	if _, err := db.Exec(`UPDATE fallback_rules SET display_identity = 'Asa' WHERE source_model = 'support'`); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set",
		`{"scope":"combo","comboId":"support","systemPrompt":null,"displayIdentity":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", rec.Code, rec.Body.String())
	}
	entry := decodeEntries(t, h)["combo:support"]
	if entry.SystemPrompt != "" || entry.DisplayIdentity != "" {
		t.Fatalf("null did not clear: %#v", entry)
	}
}

func TestPromptInjectSet_UnknownComboIs404(t *testing.T) {
	h, _ := newPromptInjectTestHandler(t)
	rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", `{"scope":"combo","comboId":"ghost","systemPrompt":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPromptInjectSet_GlobalToggleAndKeepWhitespace(t *testing.T) {
	h, _ := newPromptInjectTestHandler(t)

	prompt := "  leading and trailing whitespace kept  "
	body, _ := json.Marshal(map[string]any{"scope": "global", "systemPrompt": prompt, "enabled": true})
	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("global set = %d: %s", rec.Code, rec.Body.String())
	}

	entry := decodeEntries(t, h)["global:"]
	if entry.SystemPrompt != prompt {
		t.Fatalf("prompt whitespace was trimmed: %q", entry.SystemPrompt)
	}
	if !entry.Enabled || !entry.EnabledExplicit {
		t.Fatalf("expected explicit on, got %#v", entry)
	}
}

// The preview must come from the same engine, including template variables and
// per-format composition.
func TestPromptInjectPreview_RendersWithRealEngine(t *testing.T) {
	h, _ := newPromptInjectTestHandler(t)

	payload, _ := json.Marshal(map[string]any{
		"scope":           "combo",
		"comboId":         "support-internal",
		"systemPrompt":    "You are {{display_name}} for {{combo_name}} ({{requested_model}}).",
		"injectMode":      "replace",
		"displayIdentity": "Support Assistant",
		"requestedModel":  "support",
	})
	rec := postJSON(t, h.HandlePromptInjectPreview, "/v1/prompt-inject/preview", string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Rendered string                     `json:"rendered"`
		Enabled  bool                       `json:"enabled"`
		Mode     string                     `json:"mode"`
		Samples  map[string]json.RawMessage `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}

	want := "You are Support Assistant for support-internal (support)."
	if resp.Rendered != want {
		t.Fatalf("rendered = %q, want %q", resp.Rendered, want)
	}
	if !resp.Enabled || resp.Mode != "replace" {
		t.Fatalf("unexpected flags: enabled=%v mode=%s", resp.Enabled, resp.Mode)
	}

	var chat struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(resp.Samples["openai-chat"], &chat); err != nil {
		t.Fatalf("decode chat sample: %v", err)
	}
	if len(chat.Messages) != 2 || chat.Messages[0].Role != "system" || chat.Messages[0].Content != want {
		t.Fatalf("chat sample did not replace the client prompt: %#v", chat.Messages)
	}

	var claude struct {
		System   []map[string]any `json:"system"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(resp.Samples["claude"], &claude); err != nil {
		t.Fatalf("decode claude sample: %v", err)
	}
	if len(claude.System) != 1 || claude.System[0]["text"] != want {
		t.Fatalf("claude sample = %#v", claude.System)
	}
	if len(claude.Messages) != 1 {
		t.Fatalf("claude sample should keep only the user turn: %#v", claude.Messages)
	}

	var gemini struct {
		SystemInstruction struct {
			Parts []map[string]any `json:"parts"`
		} `json:"systemInstruction"`
	}
	if err := json.Unmarshal(resp.Samples["gemini"], &gemini); err != nil {
		t.Fatalf("decode gemini sample: %v", err)
	}
	if len(gemini.SystemInstruction.Parts) != 1 || gemini.SystemInstruction.Parts[0]["text"] != want {
		t.Fatalf("gemini sample = %#v", gemini.SystemInstruction.Parts)
	}
}

func TestPromptInjectSet_MethodGuard(t *testing.T) {
	h, _ := newPromptInjectTestHandler(t)
	rec := httptest.NewRecorder()
	h.HandlePromptInjectSet(rec, httptest.NewRequest(http.MethodGet, "/v1/prompt-inject/set", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
