package routerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The dashboard rewrites a combo by deleting every fallback rule and then
// re-inserting one per model. Whatever the combo's prompt settings were must
// survive that rewrite — that is the whole point of keeping them per combo
// instead of per rule.
func TestComboPromptSurvivesRuleRewrite(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	// :memory: sqlite is per connection; pin the pool so every handler call
	// sees the same database.
	db.SetMaxOpenConns(1)

	// 1. A combo is created with its first model.
	if rec := postJSON(t, h.HandleFallbackCreate, "/v1/settings/fallbacks",
		`{"sourceModel":"duo","targetModel":"openai/gpt-4o-mini","priority":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("create combo = %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Its prompt is configured and switched on.
	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set",
		`{"scope":"combo","comboId":"duo","systemPrompt":"You are {{display_name}}.","injectMode":"append","displayIdentity":"Duo","enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("set prompt = %d: %s", rec.Code, rec.Body.String())
	}

	// 3. A second model is added: delete every rule, re-create them all.
	rules := decodeFallbacks(t, h)
	for _, rule := range rules {
		req := httptest.NewRequest(http.MethodDelete, "/v1/settings/fallbacks/"+rule.ID, nil)
		rec := httptest.NewRecorder()
		h.HandleFallbackDelete(rec, withURLParam(req, "id", rule.ID))
		if rec.Code != http.StatusOK {
			t.Fatalf("delete rule = %d: %s", rec.Code, rec.Body.String())
		}
	}
	for i, model := range []string{"openai/gpt-4o-mini", "anthropic/claude-3-5-sonnet"} {
		body := `{"sourceModel":"duo","targetModel":"` + model + `","priority":` + itoa(i+1) + `}`
		if rec := postJSON(t, h.HandleFallbackCreate, "/v1/settings/fallbacks", body); rec.Code != http.StatusCreated {
			t.Fatalf("re-create rule = %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 4. The UI still shows the prompt, switched on, with its identity and mode.
	entry := decodeEntries(t, h)["combo:duo"]
	if entry.SystemPrompt != "You are {{display_name}}." {
		t.Fatalf("prompt lost after adding a model: %q", entry.SystemPrompt)
	}
	if entry.InjectMode != "append" || entry.DisplayIdentity != "Duo" || !entry.Enabled {
		t.Fatalf("settings drift after adding a model: mode=%q identity=%q enabled=%v",
			entry.InjectMode, entry.DisplayIdentity, entry.Enabled)
	}

	// 5. The re-created rule rows carry the settings too, so anything reading
	//    the rule columns directly sees the same value.
	var prompt, mode, identity string
	if err := db.QueryRow(`SELECT COALESCE(system_prompt,''), COALESCE(inject_mode,''), COALESCE(display_identity,'')
	                         FROM fallback_rules WHERE source_model='duo' AND target_model='anthropic/claude-3-5-sonnet'`).
		Scan(&prompt, &mode, &identity); err != nil {
		t.Fatalf("read new rule: %v", err)
	}
	if prompt != "You are {{display_name}}." || mode != "append" || identity != "Duo" {
		t.Fatalf("new rule row missing inherited settings: %q/%q/%q", prompt, mode, identity)
	}
}

// A prompt-inject write must also work while the rules are momentarily gone
// (the delete half of the rewrite), otherwise the UI's save races the edit.
func TestComboPromptSetWithNoRulesStillPersists(t *testing.T) {
	h, db := newPromptInjectTestHandler(t)
	db.SetMaxOpenConns(1)

	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set",
		`{"scope":"combo","comboId":"ghost","systemPrompt":"hi"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown combo should 404, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := db.Exec(`INSERT INTO fallback_rules (id, source_model, target_model, priority, enabled, created_at)
	                      VALUES ('fr-1','ghost','openai/gpt-4o-mini',1,1,1)`); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set",
		`{"scope":"combo","comboId":"ghost","systemPrompt":"hi"}`); rec.Code != http.StatusOK {
		t.Fatalf("set = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := db.Exec(`DELETE FROM fallback_rules WHERE source_model='ghost'`); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if rec := postJSON(t, h.HandlePromptInjectSet, "/v1/prompt-inject/set",
		`{"scope":"combo","comboId":"ghost","injectMode":"replace"}`); rec.Code != http.StatusOK {
		t.Fatalf("set on stored combo with no rules = %d: %s", rec.Code, rec.Body.String())
	}
}

func decodeFallbacks(t *testing.T, h *RouterAPIHandler) []FallbackRule {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleFallbacksGet(rec, httptest.NewRequest(http.MethodGet, "/v1/settings/fallbacks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET fallbacks = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Fallbacks []FallbackRule `json:"fallbacks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode fallbacks: %v", err)
	}
	return resp.Fallbacks
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func itoa(n int) string { return strconv.Itoa(n) }
