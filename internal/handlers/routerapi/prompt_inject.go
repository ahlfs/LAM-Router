package routerapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"lamrouter/internal/handlerutil"
	"lamrouter/internal/promptinject"
)

// promptInjectEntry adalah satu rule inject yang dikembalikan/diterima API.
type promptInjectEntry struct {
	Scope        string `json:"scope"` // "global" | "combo"
	ComboID      string `json:"comboId,omitempty"`
	ComboName    string `json:"comboName,omitempty"`
	SystemPrompt string `json:"systemPrompt"`
	InjectMode   string `json:"injectMode"`
	TargetModel  string `json:"targetModel,omitempty"`
	// Enabled is the effective flag: a stored 0/1 wins, otherwise a non-blank
	// prompt keeps legacy rows active.
	Enabled bool `json:"enabled"`
	// EnabledExplicit reports whether the flag is actually stored, so the UI can
	// tell "off on purpose" from "off because no prompt yet".
	EnabledExplicit bool   `json:"enabledExplicit"`
	DisplayIdentity string `json:"displayIdentity"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// GET /v1/prompt-inject
// Mengembalikan semua inject rules: global + tiap combo (source_model unik di fallback_rules).
func (h *RouterAPIHandler) HandlePromptInjectGet(w http.ResponseWriter, r *http.Request) {
	var entries []promptInjectEntry

	// 1. Global inject
	var globalPrompt, globalMode, globalEnabled sql.NullString
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_system_prompt'").Scan(&globalPrompt)
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_inject_mode'").Scan(&globalMode)
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_prompt_enabled'").Scan(&globalEnabled)

	globalEntry := promptInjectEntry{
		Scope:           "global",
		SystemPrompt:    globalPrompt.String,
		InjectMode:      normalizeStoredMode(globalMode.String),
		DisplayIdentity: "",
	}
	globalEntry.EnabledExplicit = globalEnabled.Valid && globalEnabled.String != ""
	globalEntry.Enabled = promptinject.ResolveEnabled(flagFromString(globalEnabled), globalEntry.SystemPrompt)
	entries = append(entries, globalEntry)

	// 2. Per-combo dari tabel combos (jika ada)
	comboRows, err := h.db.Query(`
		SELECT id, name, COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'),
		       prompt_injection_enabled, COALESCE(display_identity,''), COALESCE(updatedAt,'')
		FROM combos ORDER BY createdAt ASC
	`)
	if err == nil {
		defer comboRows.Close()
		for comboRows.Next() {
			var e promptInjectEntry
			var updatedAt string
			var enabled sql.NullInt64
			if scanErr := comboRows.Scan(&e.ComboID, &e.ComboName, &e.SystemPrompt, &e.InjectMode,
				&enabled, &e.DisplayIdentity, &updatedAt); scanErr == nil {
				e.Scope = "combo"
				e.InjectMode = normalizeStoredMode(e.InjectMode)
				e.EnabledExplicit = enabled.Valid
				e.Enabled = promptinject.ResolveEnabled(int64Ptr(enabled), e.SystemPrompt)
				if t, tErr := time.Parse(time.RFC3339, updatedAt); tErr == nil {
					e.UpdatedAt = t.UnixMilli()
				}
				entries = append(entries, e)
			}
		}
	}

	// 3. Per-fallback-rule-group: satu entry per source_model unik
	frRows, err := h.db.Query(`
		SELECT source_model,
		       COALESCE(MAX(system_prompt),''),
		       COALESCE(MAX(inject_mode),'prepend'),
		       COALESCE(GROUP_CONCAT(DISTINCT target_model),''),
		       MAX(prompt_injection_enabled),
		       COALESCE(MAX(display_identity),'')
		FROM fallback_rules
		GROUP BY source_model
		ORDER BY MIN(created_at) ASC
	`)
	if err == nil {
		defer frRows.Close()
		for frRows.Next() {
			var e promptInjectEntry
			var enabled sql.NullInt64
			if scanErr := frRows.Scan(&e.ComboID, &e.SystemPrompt, &e.InjectMode, &e.TargetModel,
				&enabled, &e.DisplayIdentity); scanErr == nil {
				e.Scope = "combo"
				e.ComboName = e.ComboID // source_model IS the combo name in fallback_rules
				e.InjectMode = normalizeStoredMode(e.InjectMode)
				e.EnabledExplicit = enabled.Valid
				e.Enabled = promptinject.ResolveEnabled(int64Ptr(enabled), e.SystemPrompt)
				entries = append(entries, e)
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object":  "prompt_inject_list",
		"entries": entries,
	})
}

// POST /v1/prompt-inject/set
//
// Body: any subset of
//
//	{ "scope": "global"|"combo", "comboId": "...", "systemPrompt": "..."|null,
//	  "injectMode": "prepend"|"append"|"replace"|"hidden_user",
//	  "enabled": true|false, "displayIdentity": "..."|null }
//
// Only the fields actually sent are written, so a partial update never clears a
// sibling setting. An explicit empty prompt or null clears the prompt.
func (h *RouterAPIHandler) HandlePromptInjectSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	patch, err := promptinject.Validate(raw)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if patch.Enabled == nil && patch.Mode == nil && patch.SystemPrompt == nil && patch.DisplayIdentity == nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid combo settings")
		return
	}

	scope := strings.TrimSpace(rawString(raw["scope"]))
	comboID := strings.TrimSpace(rawString(raw["comboId"]))

	switch scope {
	case "global":
		h.saveGlobalPromptSettings(w, patch)
	case "combo":
		if comboID == "" {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "comboId is required for scope=combo")
			return
		}
		h.saveComboPromptSettings(w, comboID, patch)
	default:
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "scope must be 'global' or 'combo'")
	}
}

func (h *RouterAPIHandler) saveGlobalPromptSettings(w http.ResponseWriter, patch *promptinject.Patch) {
	if patch.SystemPrompt != nil {
		if _, err := h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_system_prompt', ?)", *patch.SystemPrompt); err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save global prompt: "+err.Error())
			return
		}
	}
	if patch.Mode != nil {
		if _, err := h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_inject_mode', ?)", *patch.Mode); err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save global mode: "+err.Error())
			return
		}
	}
	if patch.Enabled != nil {
		if _, err := h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_prompt_enabled', ?)", boolFlag(*patch.Enabled)); err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save global switch: "+err.Error())
			return
		}
	}

	// Read back so the client renders the stored state, not its optimistic guess.
	var prompt, mode, enabled sql.NullString
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_system_prompt'").Scan(&prompt)
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_inject_mode'").Scan(&mode)
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_prompt_enabled'").Scan(&enabled)

	entry := promptInjectEntry{
		Scope:        "global",
		SystemPrompt: prompt.String,
		InjectMode:   normalizeStoredMode(mode.String),
	}
	entry.EnabledExplicit = enabled.Valid && enabled.String != ""
	entry.Enabled = promptinject.ResolveEnabled(flagFromString(enabled), entry.SystemPrompt)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "entry": entry})
}

func (h *RouterAPIHandler) saveComboPromptSettings(w http.ResponseWriter, comboID string, patch *promptinject.Patch) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Prefer the combos table (id-keyed); fall back to fallback_rules grouped by
	// source_model, which is what the UI Combo tab creates.
	if updated, err := applyPromptPatch(h.db, "combos", "id", comboID, patch, map[string]any{"updatedAt": now}); err == nil && updated {
		h.respondComboEntry(w, comboID, "combos", "id", "name")
		return
	}

	updated, frErr := applyPromptPatch(h.db, "fallback_rules", "source_model", comboID, patch, nil)
	if frErr != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save combo prompt: "+frErr.Error())
		return
	}
	if !updated {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "combo/fallback not found: "+comboID)
		return
	}
	h.respondComboEntry(w, comboID, "fallback_rules", "source_model", "")
}

// applyPromptPatch writes only the fields present in patch. extra carries columns
// that must be refreshed on every write (e.g. combos.updatedAt).
func applyPromptPatch(db *sql.DB, table, keyColumn, keyValue string, patch *promptinject.Patch, extra map[string]any) (bool, error) {
	var sets []string
	var args []any

	if patch.SystemPrompt != nil {
		sets = append(sets, "system_prompt = ?")
		args = append(args, *patch.SystemPrompt)
	}
	if patch.Mode != nil {
		sets = append(sets, "inject_mode = ?")
		args = append(args, *patch.Mode)
	}
	if patch.Enabled != nil {
		sets = append(sets, "prompt_injection_enabled = ?")
		args = append(args, boolFlag(*patch.Enabled))
	}
	if patch.DisplayIdentity != nil {
		sets = append(sets, "display_identity = ?")
		args = append(args, *patch.DisplayIdentity)
	}
	for column, value := range extra {
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if len(sets) == 0 {
		return false, nil
	}

	args = append(args, keyValue)
	query := "UPDATE " + table + " SET " + strings.Join(sets, ", ") + " WHERE " + keyColumn + " = ?"
	res, err := db.Exec(query, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// respondComboEntry reads the stored row back and returns the effective entry.
func (h *RouterAPIHandler) respondComboEntry(w http.ResponseWriter, comboID, table, keyColumn, nameColumn string) {
	entry := promptInjectEntry{Scope: "combo", ComboID: comboID, ComboName: comboID, InjectMode: "prepend"}
	var enabled sql.NullInt64
	var mode sql.NullString

	query := "SELECT COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'), prompt_injection_enabled, COALESCE(display_identity,'') FROM " +
		table + " WHERE " + keyColumn + " = ?"
	if nameColumn != "" {
		query = "SELECT COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'), prompt_injection_enabled, COALESCE(display_identity,''), COALESCE(" + nameColumn + ",'') FROM " +
			table + " WHERE " + keyColumn + " = ?"
	}

	if nameColumn != "" {
		_ = h.db.QueryRow(query, comboID).Scan(&entry.SystemPrompt, &mode, &enabled, &entry.DisplayIdentity, &entry.ComboName)
	} else {
		_ = h.db.QueryRow(query, comboID).Scan(&entry.SystemPrompt, &mode, &enabled, &entry.DisplayIdentity)
	}
	entry.InjectMode = normalizeStoredMode(mode.String)
	entry.EnabledExplicit = enabled.Valid
	entry.Enabled = promptinject.ResolveEnabled(int64Ptr(enabled), entry.SystemPrompt)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "entry": entry})
}

// POST /v1/prompt-inject/preview
//
// Renders a draft with the same engine the gateway uses, so the UI can never
// drift from real behaviour. Returns the substituted text plus one composed
// sample per supported wire format.
func (h *RouterAPIHandler) HandlePromptInjectPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Scope           string `json:"scope"`
		ComboID         string `json:"comboId"`
		SystemPrompt    string `json:"systemPrompt"`
		InjectMode      string `json:"injectMode"`
		DisplayIdentity string `json:"displayIdentity"`
		RequestedModel  string `json:"requestedModel"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	comboName := strings.TrimSpace(req.ComboID)
	requested := strings.TrimSpace(req.RequestedModel)
	if requested == "" {
		requested = comboName
	}
	enabled := promptinject.ResolveEnabled(nil, req.SystemPrompt)
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if strings.TrimSpace(comboName) == "" && req.Scope == "global" {
		comboName = ""
	}

	vars := promptinject.Vars{ComboName: comboName, DisplayIdentity: req.DisplayIdentity, RequestedModel: requested}
	settings := promptinject.Settings{
		Enabled:         enabled,
		Mode:            req.InjectMode,
		SystemPrompt:    req.SystemPrompt,
		DisplayIdentity: req.DisplayIdentity,
	}

	samples := map[string]any{}
	for format, body := range previewBodies(requested) {
		composed := promptinject.ApplyAs(body, settings, vars, format)
		var decoded any
		if err := json.Unmarshal(composed, &decoded); err != nil {
			decoded = string(composed)
		}
		samples[string(format)] = decoded
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"rendered": promptinject.Render(req.SystemPrompt, vars),
		"enabled":  enabled,
		"mode":     string(promptinject.NormalizeMode(req.InjectMode)),
		"samples":  samples,
	})
}

// previewBodies returns one minimal request per wire format, each carrying the
// same client system prompt and user turn.
func previewBodies(model string) map[promptinject.Format][]byte {
	if strings.TrimSpace(model) == "" {
		model = "preview-model"
	}
	bodies := map[promptinject.Format]any{
		promptinject.FormatChat: map[string]any{
			"model": model,
			"messages": []any{
				map[string]any{"role": "system", "content": "You are a helpful assistant."},
				map[string]any{"role": "user", "content": "Hello!"},
			},
		},
		promptinject.FormatClaude: map[string]any{
			"model":  model,
			"system": "You are a helpful assistant.",
			"messages": []any{
				map[string]any{"role": "user", "content": "Hello!"},
			},
		},
		promptinject.FormatResponses: map[string]any{
			"model":        model,
			"instructions": "You are a helpful assistant.",
			"input": []any{
				map[string]any{"role": "user", "content": "Hello!"},
			},
		},
		promptinject.FormatGemini: map[string]any{
			"model":             model,
			"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "You are a helpful assistant."}}},
			"contents": []any{
				map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Hello!"}}},
			},
		},
	}

	out := make(map[promptinject.Format][]byte, len(bodies))
	for format, body := range bodies {
		encoded, err := json.Marshal(body)
		if err != nil {
			continue
		}
		out[format] = encoded
	}
	return out
}

// ---------------------------------------------------------------- helpers

func normalizeStoredMode(mode string) string {
	return string(promptinject.NormalizeMode(mode))
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func boolFlag(v bool) int {
	if v {
		return 1
	}
	return 0
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

// flagFromString reads the 0/1 text stored in system_settings.
func flagFromString(v sql.NullString) *int64 {
	if !v.Valid || v.String == "" {
		return nil
	}
	var n int64
	switch strings.ToLower(strings.TrimSpace(v.String)) {
	case "1", "true", "on":
		n = 1
	default:
		n = 0
	}
	return &n
}
