package routerapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"lamrouter/internal/handlerutil"
)

// validInjectModes adalah set mode yang diizinkan.
var validInjectModes = map[string]bool{
	"prepend":     true,
	"append":      true,
	"replace":     true,
	"hidden_user": true,
}

// promptInjectEntry adalah satu rule inject yang dikembalikan/diterima API.
type promptInjectEntry struct {
	Scope        string `json:"scope"` // "global" | "combo"
	ComboID      string `json:"comboId,omitempty"`
	ComboName    string `json:"comboName,omitempty"`
	SystemPrompt string `json:"systemPrompt"`
	InjectMode   string `json:"injectMode"`
	TargetModel  string `json:"targetModel,omitempty"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// GET /v1/prompt-inject
// Mengembalikan semua inject rules: global + tiap combo (source_model unik di fallback_rules).
func (h *RouterAPIHandler) HandlePromptInjectGet(w http.ResponseWriter, r *http.Request) {
	var entries []promptInjectEntry

	// 1. Global inject
	var globalPrompt, globalMode string
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_system_prompt'").Scan(&globalPrompt)
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'global_inject_mode'").Scan(&globalMode)
	if globalMode == "" {
		globalMode = "prepend"
	}
	entries = append(entries, promptInjectEntry{
		Scope:        "global",
		SystemPrompt: globalPrompt,
		InjectMode:   globalMode,
	})

	// 2. Per-combo dari tabel combos (jika ada)
	comboRows, err := h.db.Query(`
		SELECT id, name, COALESCE(system_prompt,''), COALESCE(inject_mode,'prepend'), COALESCE(updatedAt,'')
		FROM combos ORDER BY createdAt ASC
	`)
	if err == nil {
		defer comboRows.Close()
		for comboRows.Next() {
			var e promptInjectEntry
			var updatedAt string
			if scanErr := comboRows.Scan(&e.ComboID, &e.ComboName, &e.SystemPrompt, &e.InjectMode, &updatedAt); scanErr == nil {
				e.Scope = "combo"
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
		       COALESCE(GROUP_CONCAT(DISTINCT target_model),'')
		FROM fallback_rules
		GROUP BY source_model
		ORDER BY MIN(created_at) ASC
	`)
	if err == nil {
		defer frRows.Close()
		for frRows.Next() {
			var e promptInjectEntry
			if scanErr := frRows.Scan(&e.ComboID, &e.SystemPrompt, &e.InjectMode, &e.TargetModel); scanErr == nil {
				e.Scope = "combo"
				e.ComboName = e.ComboID // source_model IS the combo name in fallback_rules
				entries = append(entries, e)
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object":  "prompt_inject_list",
		"entries": entries,
	})
}

// POST /v1/prompt-inject
// Body: { "scope": "global"|"combo", "comboId": "...", "systemPrompt": "...", "injectMode": "prepend"|"append"|"replace"|"hidden_user" }
// systemPrompt kosong = hapus inject untuk scope tersebut.
func (h *RouterAPIHandler) HandlePromptInjectSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Scope        string `json:"scope"`
		ComboID      string `json:"comboId"`
		SystemPrompt string `json:"systemPrompt"`
		InjectMode   string `json:"injectMode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req.Scope = strings.TrimSpace(req.Scope)
	req.SystemPrompt = strings.TrimSpace(req.SystemPrompt)
	if req.InjectMode == "" {
		req.InjectMode = "prepend"
	}
	if !validInjectModes[req.InjectMode] {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "injectMode must be one of: prepend, append, replace, hidden_user")
		return
	}

	switch req.Scope {
	case "global":
		_, err := h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_system_prompt', ?)", req.SystemPrompt)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save global prompt: "+err.Error())
			return
		}
		_, _ = h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('global_inject_mode', ?)", req.InjectMode)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success":      true,
			"scope":        "global",
			"systemPrompt": req.SystemPrompt,
			"injectMode":   req.InjectMode,
		})

	case "combo":
		if req.ComboID == "" {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "comboId is required for scope=combo")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)

		// Coba update di tabel combos dulu
		comboRes, comboErr := h.db.Exec(
			"UPDATE combos SET system_prompt = ?, inject_mode = ?, updatedAt = ? WHERE id = ?",
			req.SystemPrompt, req.InjectMode, now, req.ComboID,
		)
		if comboErr == nil {
			if n, _ := comboRes.RowsAffected(); n > 0 {
				var comboName string
				_ = h.db.QueryRow("SELECT name FROM combos WHERE id = ?", req.ComboID).Scan(&comboName)
				handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
					"success":      true,
					"scope":        "combo",
					"comboId":      req.ComboID,
					"comboName":    comboName,
					"systemPrompt": req.SystemPrompt,
					"injectMode":   req.InjectMode,
				})
				return
			}
		}

		// Fallback: update fallback_rules berdasarkan source_model = comboId
		frRes, frErr := h.db.Exec(
			"UPDATE fallback_rules SET system_prompt = ?, inject_mode = ? WHERE source_model = ?",
			req.SystemPrompt, req.InjectMode, req.ComboID,
		)
		if frErr != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save combo prompt: "+frErr.Error())
			return
		}
		if n, _ := frRes.RowsAffected(); n == 0 {
			handlerutil.WriteJSONError(w, http.StatusNotFound, "combo/fallback not found: "+req.ComboID)
			return
		}
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success":      true,
			"scope":        "combo",
			"comboId":      req.ComboID,
			"comboName":    req.ComboID,
			"systemPrompt": req.SystemPrompt,
			"injectMode":   req.InjectMode,
		})

	default:
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "scope must be 'global' or 'combo'")
	}
}
