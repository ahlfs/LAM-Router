package chat

import (
	"testing"

	"lamrouter/internal/db"
	"lamrouter/internal/promptinject"
)

// The dashboard adds a model to a combo by deleting every rule and re-inserting
// them with only model/priority fields. Resolution must still inject the combo's
// prompt afterwards — the prompt belongs to the combo, not to a rule row.
func TestResolveModel_ComboPromptSurvivesRuleRewrite(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	ensurePromptSchema(t, database)
	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	seed := func(id, target, prompt string) {
		t.Helper()
		if _, err := database.Exec(`INSERT INTO fallback_rules
			(id, source_model, target_model, priority, enabled, created_at, system_prompt, inject_mode, prompt_injection_enabled)
			VALUES (?, 'duo', ?, 1, 1, 1, ?, 'append', 1)`, id, target, prompt); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
	}
	seed("fr-1", "openai/gpt-4o-mini", "You are {{combo_name}}.")

	prompt := "You are {{combo_name}}."
	enabled := true
	mode := string(promptinject.ModeAppend)
	if _, err := promptinject.SaveComboSettings(database, "duo", promptinject.Patch{
		SystemPrompt: &prompt, Mode: &mode, Enabled: &enabled,
	}); err != nil {
		t.Fatalf("save combo settings: %v", err)
	}

	info, err := h.resolveModel("duo")
	if err != nil || info == nil {
		t.Fatalf("resolve before rewrite: %v", err)
	}
	if info.SystemPrompt != prompt || !info.InjectEnabled {
		t.Fatalf("prompt missing before rewrite: %+v", info)
	}

	// The rewrite: drop every rule, then insert the models again bare.
	if _, err := database.Exec(`DELETE FROM fallback_rules WHERE source_model = 'duo'`); err != nil {
		t.Fatalf("delete rules: %v", err)
	}
	for _, r := range []struct{ id, target string }{
		{"fr-2", "openai/gpt-4o-mini"},
		{"fr-3", "anthropic/claude-3-5-sonnet"},
	} {
		if _, err := database.Exec(`INSERT INTO fallback_rules
			(id, source_model, target_model, priority, enabled, created_at)
			VALUES (?, 'duo', ?, 1, 1, 1)`, r.id, r.target); err != nil {
			t.Fatalf("re-insert rule: %v", err)
		}
	}

	info, err = h.resolveModel("duo")
	if err != nil || info == nil {
		t.Fatalf("resolve after rewrite: %v", err)
	}
	if info.SystemPrompt != prompt {
		t.Fatalf("prompt lost after adding a model: %q", info.SystemPrompt)
	}
	if !info.InjectEnabled {
		t.Fatal("injection switched off after adding a model")
	}
	if info.InjectMode != string(promptinject.ModeAppend) {
		t.Fatalf("inject mode reset after adding a model: %q", info.InjectMode)
	}
	if info.ComboName != "duo" {
		t.Fatalf("combo name lost: %q", info.ComboName)
	}
	if len(info.ComboModels) != 2 {
		t.Fatalf("expected both models after the rewrite, got %v", info.ComboModels)
	}
}
