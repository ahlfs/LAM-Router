package promptinject

import (
	"database/sql"
	"strings"
	"time"
)

// ComboSettingsSchema creates the durable per-combo injection store.
//
// Combo settings must not live on fallback_rules rows. The dashboard rewrites a
// combo by deleting every rule and re-inserting them with only model/priority
// fields, so anything stored solely on a rule row (system_prompt, inject_mode,
// prompt_injection_enabled, display_identity) is destroyed the moment a model is
// added to or removed from the combo. One row per combo name survives that
// rewrite: rules are disposable, the combo is not.
const ComboSettingsSchema = `CREATE TABLE IF NOT EXISTS combo_prompt_settings (
    combo_name TEXT PRIMARY KEY,
    system_prompt TEXT NOT NULL DEFAULT '',
    inject_mode TEXT NOT NULL DEFAULT 'prepend',
    prompt_injection_enabled INTEGER,
    display_identity TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
)`

// ComboSettings is one combo's stored injection configuration.
type ComboSettings struct {
	ComboName       string
	SystemPrompt    string
	InjectMode      string
	Enabled         *int64 // nil means "not stored yet" (legacy semantics)
	DisplayIdentity string
	UpdatedAt       int64 // unix milli
}

// EffectiveEnabled resolves the stored flag against prompt presence.
func (c ComboSettings) EffectiveEnabled() bool {
	return ResolveEnabled(c.Enabled, c.SystemPrompt)
}

// IsEmpty reports whether the row carries no information at all. Callers treat
// an empty row as "nothing stored" and fall back to the legacy rule columns.
func (c ComboSettings) IsEmpty() bool {
	return strings.TrimSpace(c.SystemPrompt) == "" &&
		strings.TrimSpace(c.DisplayIdentity) == "" &&
		c.Enabled == nil
}

// EnsureComboSettingsTable creates the store if it is missing. InitSchema runs
// this at startup; write paths call it too so a database created before the
// upgrade can never 500 on an insert.
func EnsureComboSettingsTable(db *sql.DB) error {
	if db == nil {
		return sql.ErrConnDone
	}
	_, err := db.Exec(ComboSettingsSchema)
	return err
}

// LoadComboSettings reads the stored row for a combo name. Missing table or row
// is reported as ok=false rather than an error: callers are on the read path.
func LoadComboSettings(db *sql.DB, comboName string) (ComboSettings, bool) {
	name := strings.TrimSpace(comboName)
	if db == nil || name == "" {
		return ComboSettings{}, false
	}

	out := ComboSettings{ComboName: name}
	var prompt, mode, identity sql.NullString
	var enabled, updated sql.NullInt64

	err := db.QueryRow(`
		SELECT COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'),
		       prompt_injection_enabled, COALESCE(display_identity,''), COALESCE(updated_at,0)
		  FROM combo_prompt_settings WHERE combo_name = ?`, name).
		Scan(&prompt, &mode, &enabled, &identity, &updated)
	if err != nil {
		return ComboSettings{}, false
	}

	out.SystemPrompt = prompt.String
	out.InjectMode = string(NormalizeMode(mode.String))
	out.DisplayIdentity = identity.String
	out.UpdatedAt = updated.Int64
	if enabled.Valid {
		value := enabled.Int64
		out.Enabled = &value
	}
	return out, true
}

// SaveComboSettings applies a partial patch to the stored row and returns the
// merged result. Fields absent from the patch keep their stored value, so a
// mode-only update never clears the prompt.
//
// base (optional) seeds a missing row: a combo whose prompt still lives only in
// the legacy rule columns must be carried in, or the first patch that names a
// single field would blank the rest.
func SaveComboSettings(db *sql.DB, comboName string, patch Patch, base ...ComboSettings) (ComboSettings, error) {
	name := strings.TrimSpace(comboName)
	if err := EnsureComboSettingsTable(db); err != nil {
		return ComboSettings{}, err
	}

	current, ok := LoadComboSettings(db, name)
	if !ok {
		if len(base) > 0 {
			current = base[0]
			current.ComboName = name
		} else {
			current = ComboSettings{ComboName: name, InjectMode: string(ModePrepend)}
		}
	}
	if current.InjectMode == "" {
		current.InjectMode = string(ModePrepend)
	}

	if patch.SystemPrompt != nil {
		current.SystemPrompt = *patch.SystemPrompt
	}
	if patch.Mode != nil {
		current.InjectMode = string(NormalizeMode(*patch.Mode))
	}
	if patch.Enabled != nil {
		value := int64(0)
		if *patch.Enabled {
			value = 1
		}
		current.Enabled = &value
	}
	if patch.DisplayIdentity != nil {
		current.DisplayIdentity = *patch.DisplayIdentity
	}

	current.UpdatedAt = time.Now().UnixMilli()

	var enabled any
	if current.Enabled != nil {
		enabled = *current.Enabled
	}

	_, err := db.Exec(`
		INSERT OR REPLACE INTO combo_prompt_settings
		    (combo_name, system_prompt, inject_mode, prompt_injection_enabled, display_identity, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		name, current.SystemPrompt, string(NormalizeMode(current.InjectMode)), enabled,
		current.DisplayIdentity, current.UpdatedAt)
	if err != nil {
		return ComboSettings{}, err
	}
	return current, nil
}

// LoadAllComboSettings reads the whole store in one pass. List handlers must use
// this instead of a per-entry query: issuing a query while another rowset is
// still open on the same *sql.DB can starve the pool and deadlock.
func LoadAllComboSettings(db *sql.DB) map[string]ComboSettings {
	out := map[string]ComboSettings{}
	if db == nil {
		return out
	}
	rows, err := db.Query(`
		SELECT combo_name, COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'),
		       prompt_injection_enabled, COALESCE(display_identity,''), COALESCE(updated_at,0)
		  FROM combo_prompt_settings`)
	if err != nil {
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var settings ComboSettings
		var enabled sql.NullInt64
		var mode sql.NullString
		if err := rows.Scan(&settings.ComboName, &settings.SystemPrompt, &mode,
			&enabled, &settings.DisplayIdentity, &settings.UpdatedAt); err != nil {
			continue
		}
		settings.InjectMode = string(NormalizeMode(mode.String))
		if enabled.Valid {
			value := enabled.Int64
			settings.Enabled = &value
		}
		out[strings.TrimSpace(settings.ComboName)] = settings
	}
	return out
}

// BackfillComboSettings seeds the store from the legacy per-rule columns once,
// so prompts written before the store existed keep injecting. It no-ops as soon
// as the store has any row: from then on the store is authoritative and a
// deliberate clear must never be resurrected by an old rule row.
func BackfillComboSettings(db *sql.DB) error {
	if err := EnsureComboSettingsTable(db); err != nil {
		return err
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM combo_prompt_settings`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UnixMilli()
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO combo_prompt_settings
		    (combo_name, system_prompt, inject_mode, prompt_injection_enabled, display_identity, updated_at)
		SELECT source_model,
		       COALESCE(MAX(NULLIF(system_prompt,'')), ''),
		       COALESCE(MAX(NULLIF(NULLIF(inject_mode,''),'prepend')), 'prepend'),
		       MAX(prompt_injection_enabled),
		       COALESCE(MAX(NULLIF(display_identity,'')), ''),
		       ?
		  FROM fallback_rules
		 GROUP BY source_model
		HAVING COALESCE(MAX(NULLIF(system_prompt,'')), '') <> ''
		    OR COALESCE(MAX(NULLIF(display_identity,'')), '') <> ''
		    OR MAX(prompt_injection_enabled) IS NOT NULL`, now); err != nil {
		return err
	}

	// Builds that still carry a combos table key the same settings by name.
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO combo_prompt_settings
		    (combo_name, system_prompt, inject_mode, prompt_injection_enabled, display_identity, updated_at)
		SELECT name,
		       COALESCE(system_prompt, ''),
		       COALESCE(NULLIF(NULLIF(inject_mode,''),'prepend'), 'prepend'),
		       prompt_injection_enabled,
		       COALESCE(display_identity, ''),
		       ?
		  FROM combos
		 WHERE COALESCE(system_prompt,'') <> ''
		    OR COALESCE(display_identity,'') <> ''
		    OR prompt_injection_enabled IS NOT NULL`, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return err
	}
	return nil
}
