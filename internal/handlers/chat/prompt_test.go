package chat

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"lamrouter/internal/db"
	"lamrouter/internal/promptinject"
)

// ensurePromptSchema adds the prompt-inject columns that the shared test schema
// predates, mirroring the runtime migration in routerapi.InitSchema.
func ensurePromptSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	stmts := []string{
		`ALTER TABLE combos ADD COLUMN strategy TEXT`,
		`ALTER TABLE combos ADD COLUMN system_prompt TEXT DEFAULT ''`,
		`ALTER TABLE combos ADD COLUMN inject_mode TEXT DEFAULT 'prepend'`,
		`ALTER TABLE combos ADD COLUMN prompt_injection_enabled INTEGER`,
		`ALTER TABLE combos ADD COLUMN display_identity TEXT`,
		`CREATE TABLE IF NOT EXISTS fallback_rules (
			id TEXT PRIMARY KEY,
			source_model TEXT NOT NULL,
			target_model TEXT NOT NULL,
			priority INTEGER DEFAULT 0,
			enabled INTEGER DEFAULT 1,
			trigger_on_status TEXT,
			max_retries INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			system_prompt TEXT DEFAULT '',
			inject_mode TEXT DEFAULT 'prepend',
			prompt_injection_enabled INTEGER,
			display_identity TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS system_settings (key TEXT PRIMARY KEY, value TEXT)`,
	}
	for _, stmt := range stmts {
		if _, err := database.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			t.Fatalf("schema step failed (%s): %v", stmt, err)
		}
	}
}

func setGlobalPrompt(t *testing.T, database *sql.DB, prompt, mode string) {
	t.Helper()
	if _, err := database.Exec(`INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_system_prompt', ?)`, prompt); err != nil {
		t.Fatalf("seed global prompt: %v", err)
	}
	if _, err := database.Exec(`INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_inject_mode', ?)`, mode); err != nil {
		t.Fatalf("seed global mode: %v", err)
	}
}

// seedPromptCombo inserts a combo carrying prompt settings. enabled may be nil
// to emulate a row written before the flag existed.
func seedPromptCombo(t *testing.T, database *sql.DB, name, prompt, mode, identity string, enabled *int64) {
	t.Helper()
	models, _ := json.Marshal([]string{"deepseek/deepseek-chat"})
	if _, err := database.Exec(
		`INSERT INTO combos (id, name, kind, models, createdAt, updatedAt, system_prompt, inject_mode, display_identity, prompt_injection_enabled)
		 VALUES (?, ?, 'fallback', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?, ?, ?, ?)`,
		"combo-"+name, name, string(models), prompt, mode, identity, enabled,
	); err != nil {
		t.Fatalf("seed combo %s: %v", name, err)
	}
}

func messageRoles(t *testing.T, body []byte) []string {
	t.Helper()
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	roles := make([]string, 0, len(payload.Messages))
	for _, m := range payload.Messages {
		roles = append(roles, m.Role)
	}
	return roles
}

func systemContents(t *testing.T, body []byte) []string {
	t.Helper()
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	var out []string
	for _, m := range payload.Messages {
		if m.Role == "system" {
			out = append(out, m.Content)
		}
	}
	return out
}

// ------------------------------------------------------------ ordering rules

func TestPromptInjectionPlacesComboAboveGlobal(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	setGlobalPrompt(t, database, "GLOBAL-RULE", "prepend")

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	info := &ModelInfo{InjectEnabled: true, SystemPrompt: "COMBO-RULE", InjectMode: "prepend", ComboName: "smart"}

	out := handler.applyPromptInjection(body, info, "smart", promptinject.FormatChat)
	got := systemContents(t, out)
	want := []string{"COMBO-RULE", "GLOBAL-RULE"}
	if len(got) != len(want) {
		t.Fatalf("system messages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("system order = %v, want %v", got, want)
		}
	}
}

func TestPromptInjectionDisabledComboLeavesBodyUntouched(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	info := &ModelInfo{InjectEnabled: false, SystemPrompt: "SHOULD-NOT-APPEAR", InjectMode: "prepend"}

	out := handler.applyPromptInjection(body, info, "m", promptinject.FormatChat)
	if &out[0] != &body[0] {
		t.Fatalf("disabled scope should hand back the original slice, got %s", out)
	}
}

// ------------------------------------------------------- legacy rows & vars

func TestLegacyComboPromptStaysActiveWithoutFlag(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	seedPromptCombo(t, database, "legacy", "LEGACY-RULE", "prepend", "", nil)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	info, err := handler.resolveModel("legacy")
	if err != nil {
		t.Fatalf("resolve combo: %v", err)
	}
	if !info.InjectEnabled {
		t.Fatalf("legacy row with a prompt must stay enabled")
	}
	out := handler.applyPromptInjection([]byte(`{"model":"legacy","messages":[{"role":"user","content":"hi"}]}`), info, "legacy", promptinject.FormatChat)
	if got := systemContents(t, out); len(got) != 1 || got[0] != "LEGACY-RULE" {
		t.Fatalf("legacy prompt not injected: %v", got)
	}
}

func TestComboFlagOffStopsInjectionEvenWithPrompt(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	off := int64(0)
	seedPromptCombo(t, database, "muted", "MUTED-RULE", "prepend", "", &off)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	info, err := handler.resolveModel("muted")
	if err != nil {
		t.Fatalf("resolve combo: %v", err)
	}
	if info.InjectEnabled {
		t.Fatalf("explicit 0 must disable injection")
	}
	body := []byte(`{"model":"muted","messages":[{"role":"user","content":"hi"}]}`)
	if out := handler.applyPromptInjection(body, info, "muted", promptinject.FormatChat); &out[0] != &body[0] {
		t.Fatalf("disabled combo should leave the body untouched")
	}
}

func TestPromptInjectionRendersTemplateVariables(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	info := &ModelInfo{
		InjectEnabled:   true,
		SystemPrompt:    "You are {{display_name}} for {{combo_name}} ({{requested_model}}).",
		InjectMode:      "prepend",
		DisplayIdentity: "Support Assistant",
		ComboName:       "support-internal",
	}
	out := handler.applyPromptInjection(
		[]byte(`{"model":"support","messages":[{"role":"user","content":"hi"}]}`),
		info, "support", promptinject.FormatChat,
	)
	got := systemContents(t, out)
	want := "You are Support Assistant for support-internal (support)."
	if len(got) != 1 || got[0] != want {
		t.Fatalf("rendered = %v, want %q", got, want)
	}
}

// -------------------------------------------------- retries must not stack

func TestPromptInjectionIsNotReappliedOnAccountFallback(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	setGlobalPrompt(t, database, "GLOBAL-RULE", "prepend")

	var mu sync.Mutex
	bodies := make([][]byte, 0, 2)

	record := func(w http.ResponseWriter, r *http.Request, status int, reply string) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusUnauthorized, `{"error":{"message":"invalid api key","type":"auth_error"}}`)
	}))
	defer failing.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"id":"r1","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer healthy.Close()

	// Park the seeded connections so only our two are candidates.
	if _, err := database.Exec(`UPDATE providerConnections SET isActive = 0 WHERE provider = 'deepseek'`); err != nil {
		t.Fatalf("park seeded connections: %v", err)
	}
	for _, conn := range []struct {
		id, name string
		prio     int
		baseURL  string
	}{{"conn-prompt-fail", "Failing", 0, failing.URL}, {"conn-prompt-ok", "Healthy", 1, healthy.URL}} {
		data, _ := json.Marshal(map[string]any{"apiKey": "sk-test", "baseUrl": conn.baseURL})
		if _, err := database.Exec(
			`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
			 VALUES (?, 'deepseek', 'apikey', ?, ?, 1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			conn.id, conn.name, conn.prio, string(data),
		); err != nil {
			t.Fatalf("insert connection %s: %v", conn.id, err)
		}
	}

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	req := httptest.NewRequest("POST", "/chat/completions",
		strings.NewReader(`{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hello"}],"stream":false}`))
	rec := httptest.NewRecorder()
	handler.HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via fallback, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("expected both attempts to reach upstream, got %d", len(bodies))
	}
	for i, body := range bodies {
		if count := strings.Count(string(body), "GLOBAL-RULE"); count != 1 {
			t.Errorf("attempt %d carried %d injections, want exactly 1: %s", i+1, count, body)
		}
	}
}

func TestPromptInjectionGlobalOffLeavesBodyUntouched(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	out := handler.applyPromptInjection(body, &ModelInfo{}, "m", promptinject.FormatChat)
	if &out[0] != &body[0] {
		t.Fatalf("no configured scope should leave the body untouched")
	}
}

// ------------------------------------------------- Claude /v1/messages path

// seedMessagesEndpoint prepares a combo whose member is an OpenAI-shaped
// provider, so the Claude request gets translated before it reaches upstream.
func seedMessagesEndpoint(t *testing.T, database *sql.DB, source, prompt, mode, identity string, enabled int64) *[]byte {
	t.Helper()
	var mu sync.Mutex
	var last []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = raw
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"r1","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(upstream.Close)

	if _, err := database.Exec(`UPDATE providerConnections SET isActive = 0 WHERE provider = 'deepseek'`); err != nil {
		t.Fatalf("park seeded connections: %v", err)
	}
	data, _ := json.Marshal(map[string]any{"apiKey": "sk-test", "baseUrl": upstream.URL})
	if _, err := database.Exec(
		`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
		 VALUES ('conn-msg', 'deepseek', 'apikey', 'Msg', 0, 1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		string(data),
	); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	if _, err := database.Exec(
		`INSERT INTO fallback_rules (id, source_model, target_model, priority, enabled, trigger_on_status, max_retries, created_at, system_prompt, inject_mode, prompt_injection_enabled, display_identity)
		 VALUES ('fr-'+?, ?, 'deepseek/deepseek-chat', 1, 1, '429', 1, '2026-01-01T00:00:00Z', ?, ?, ?, ?)`,
		source, source, prompt, mode, enabled, identity,
	); err != nil {
		t.Fatalf("seed fallback rule: %v", err)
	}

	ptr := &last
	return ptr
}

func claudeBody(model string) string {
	return `{"model":"` + model + `","max_tokens":32,"system":"CLIENT-RULE","messages":[{"role":"user","content":"hi"}]}`
}

func TestMessagesEndpointInjectsIntoClientSystem(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	last := seedMessagesEndpoint(t, database, "msgcombo", "COMBO[{{display_name}}]", "prepend", "Asa", 1)

	repo := db.NewRepo(database)
	handler := NewChatHandler(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(claudeBody("msgcombo")))
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("messages = %d: %s", rec.Code, rec.Body.String())
	}

	var upstreamReq struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*last, &upstreamReq); err != nil {
		t.Fatalf("decode upstream body: %v (%s)", err, *last)
	}
	if upstreamReq.Model != "deepseek-chat" {
		t.Fatalf("member model was not preserved: %q", upstreamReq.Model)
	}
	if len(upstreamReq.Messages) == 0 || upstreamReq.Messages[0].Role != "system" {
		t.Fatalf("no system message reached upstream: %s", *last)
	}
	if got := upstreamReq.Messages[0].Content; got != "COMBO[Asa]\nCLIENT-RULE" {
		t.Fatalf("system content = %q, want injection above the client rule", got)
	}
	if len(upstreamReq.Messages) != 2 || upstreamReq.Messages[1].Content != "hi" {
		t.Fatalf("conversation turns were not preserved: %#v", upstreamReq.Messages)
	}
}

func TestMessagesEndpointReplaceDropsClientSystem(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	last := seedMessagesEndpoint(t, database, "msgreplace", "ONLY[{{combo_name}}]", "replace", "", 1)

	handler := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(claudeBody("msgreplace"))))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages = %d: %s", rec.Code, rec.Body.String())
	}

	var upstreamReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*last, &upstreamReq); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	if got := upstreamReq.Messages[0].Content; got != "ONLY[msgreplace]" {
		t.Fatalf("replace kept the client rule: %q", got)
	}
}

func TestMessagesEndpointLeavesBodyWhenScopeDisabled(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	last := seedMessagesEndpoint(t, database, "msgoff", "SHOULD-NOT-APPEAR", "prepend", "", 0)

	handler := NewChatHandler(db.NewRepo(database))
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(claudeBody("msgoff"))))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(string(*last), "SHOULD-NOT-APPEAR") {
		t.Fatalf("disabled scope leaked into the upstream body: %s", *last)
	}
}

// The prompt scope is the combo. A request that names a provider/model directly
// never touches the combo row, so a combo prompt cannot reach it — this is the
// #1 reason "prompt inject doesn't work" reports turn out to be a scope mismatch.
func TestComboPromptDoesNotApplyToDirectModelRequests(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	seedPromptCombo(t, database, "lopp", "PERSONA-RULE", "prepend", "Asa", nil)

	handler := NewChatHandler(db.NewRepo(database))

	comboInfo, err := handler.resolveModel("lopp")
	if err != nil {
		t.Fatalf("resolve combo: %v", err)
	}
	if !comboInfo.InjectEnabled || comboInfo.SystemPrompt != "PERSONA-RULE" {
		t.Fatalf("combo scope lost its prompt: %+v", comboInfo)
	}

	directInfo, err := handler.resolveModel("deepseek/deepseek-chat")
	if err != nil {
		t.Fatalf("resolve direct: %v", err)
	}
	if directInfo.InjectEnabled || directInfo.SystemPrompt != "" {
		t.Fatalf("a direct provider/model request must not inherit a combo prompt: %+v", directInfo)
	}

	body := []byte(`{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`)
	if out := handler.applyPromptInjection(body, directInfo, "deepseek/deepseek-chat", promptinject.FormatChat); &out[0] != &body[0] {
		t.Fatalf("direct request got mutated: %s", out)
	}
}
