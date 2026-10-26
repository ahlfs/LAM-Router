package promptinject

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// :memory: is per connection; a single connection keeps the schema visible.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE fallback_rules (
		id TEXT PRIMARY KEY,
		source_model TEXT NOT NULL,
		target_model TEXT NOT NULL,
		priority INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL,
		system_prompt TEXT DEFAULT '',
		inject_mode TEXT DEFAULT 'prepend',
		prompt_injection_enabled INTEGER,
		display_identity TEXT
	)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSaveComboSettings_PartialPatchKeepsSiblings(t *testing.T) {
	db := newStoreTestDB(t)

	if _, err := SaveComboSettings(db, "combo", Patch{SystemPrompt: strPtr("keep me"), DisplayIdentity: strPtr("Combo")}); err != nil {
		t.Fatalf("save: %v", err)
	}
	settings, err := SaveComboSettings(db, "combo", Patch{Mode: strPtr(string(ModeAppend))})
	if err != nil {
		t.Fatalf("save mode: %v", err)
	}
	if settings.SystemPrompt != "keep me" || settings.DisplayIdentity != "Combo" {
		t.Fatalf("partial patch cleared siblings: %+v", settings)
	}
	if settings.InjectMode != string(ModeAppend) {
		t.Fatalf("mode not applied: %q", settings.InjectMode)
	}
}

func TestSaveComboSettings_ClearPromptSticks(t *testing.T) {
	db := newStoreTestDB(t)
	if _, err := SaveComboSettings(db, "combo", Patch{SystemPrompt: strPtr("gone")}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := SaveComboSettings(db, "combo", Patch{SystemPrompt: strPtr("")}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	settings, ok := LoadComboSettings(db, "combo")
	if !ok {
		t.Fatal("row should still exist after a clear")
	}
	if settings.SystemPrompt != "" || settings.EffectiveEnabled() {
		t.Fatalf("clear did not stick: %+v", settings)
	}

	// A later backfill must not resurrect the cleared prompt from rule columns.
	if _, err := db.Exec(`INSERT INTO fallback_rules (id, source_model, target_model, created_at, system_prompt)
	                      VALUES ('fr','combo','openai/gpt-4o-mini',1,'gone')`); err != nil {
		t.Fatal(err)
	}
	if err := BackfillComboSettings(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if settings, _ := LoadComboSettings(db, "combo"); settings.SystemPrompt != "" {
		t.Fatalf("backfill resurrected a cleared prompt: %q", settings.SystemPrompt)
	}
}

func TestBackfillComboSettings_SeedsLegacyRows(t *testing.T) {
	db := newStoreTestDB(t)
	for _, row := range []struct {
		id, source, prompt, mode, identity string
		enabled                            any
	}{
		{"fr1", "kept", "legacy prompt", "append", "Legacy", nil},
		{"fr2", "kept", "", "prepend", "", nil},         // sibling without settings
		{"fr3", "empty", "", "prepend", "", nil},        // nothing to seed
		{"fr4", "flagged", "", "prepend", "", int64(0)}, // explicit off, no prompt
	} {
		if _, err := db.Exec(`INSERT INTO fallback_rules
			(id, source_model, target_model, created_at, system_prompt, inject_mode, prompt_injection_enabled, display_identity)
			VALUES (?, ?, 'openai/gpt-4o-mini', 1, ?, ?, ?, ?)`,
			row.id, row.source, row.prompt, row.mode, row.enabled, row.identity); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	if err := BackfillComboSettings(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	kept, ok := LoadComboSettings(db, "kept")
	if !ok {
		t.Fatal("legacy combo was not seeded")
	}
	if kept.SystemPrompt != "legacy prompt" || kept.InjectMode != "append" || kept.DisplayIdentity != "Legacy" {
		t.Fatalf("seeded row wrong: %+v", kept)
	}
	if !kept.EffectiveEnabled() {
		t.Fatal("legacy prompt should read as enabled")
	}

	if _, ok := LoadComboSettings(db, "empty"); ok {
		t.Fatal("combo without settings should not be seeded")
	}

	flagged, ok := LoadComboSettings(db, "flagged")
	if !ok || flagged.EffectiveEnabled() {
		t.Fatalf("explicit-off flag must survive the seed: %+v ok=%v", flagged, ok)
	}

	// Idempotent: a second run changes nothing.
	if err := BackfillComboSettings(db); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if again, _ := LoadComboSettings(db, "kept"); again.SystemPrompt != "legacy prompt" {
		t.Fatalf("second backfill changed the row: %+v", again)
	}
}

func TestLoadAllComboSettings(t *testing.T) {
	db := newStoreTestDB(t)
	if _, err := SaveComboSettings(db, "a", Patch{SystemPrompt: strPtr("A")}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveComboSettings(db, "b", Patch{SystemPrompt: strPtr("B")}); err != nil {
		t.Fatal(err)
	}
	all := LoadAllComboSettings(db)
	if len(all) != 2 || all["a"].SystemPrompt != "A" || all["b"].SystemPrompt != "B" {
		t.Fatalf("preload mismatch: %+v", all)
	}
}

func strPtr(s string) *string { return &s }
