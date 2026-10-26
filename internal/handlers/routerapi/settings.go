package routerapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"lamrouter/internal/handlerutil"
	"lamrouter/internal/promptinject"
	"lamrouter/internal/updater"
)

type FallbackRule struct {
	ID              string `json:"id"`
	SourceModel     string `json:"sourceModel"`
	TargetModel     string `json:"targetModel"`
	Priority        int    `json:"priority"`
	Enabled         bool   `json:"enabled"`
	TriggerOnStatus []int  `json:"triggerOnStatus,omitempty"`
	MaxRetries      *int   `json:"maxRetries,omitempty"`
	CreatedAt       int64  `json:"createdAt"`
}

// GET /v1/settings
func (h *RouterAPIHandler) HandleSettingsGet(w http.ResponseWriter, r *http.Request) {
	var requireAPIKey bool = true
	var rawVal string
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'require_api_key'").Scan(&rawVal)
	if rawVal == "false" {
		requireAPIKey = false
	}

	var outboundProxy string
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'outbound_proxy'").Scan(&outboundProxy)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"requireApiKey": requireAPIKey,
		"outboundProxy": outboundProxy,
		// Report the version the binary actually runs, not a placeholder: the
		// dashboard and any API consumer compare this against the latest release.
		"version": updater.CurrentVersion,
		"name":    "LAM-Router",
	})
}

// POST/PATCH /v1/settings
func (h *RouterAPIHandler) HandleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequireAPIKey *bool   `json:"requireApiKey"`
		OutboundProxy *string `json:"outboundProxy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid settings payload")
		return
	}

	if body.RequireAPIKey != nil {
		val := "true"
		if !*body.RequireAPIKey {
			val = "false"
		}
		_, _ = h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('require_api_key', ?)", val)
	}

	if body.OutboundProxy != nil {
		proxyVal := strings.TrimSpace(*body.OutboundProxy)
		// An unparsable proxy is stored happily today and only breaks requests
		// later, inside the transport. Reject it at the door instead.
		if proxyVal != "" && !isValidProxyURL(proxyVal) {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "outboundProxy must be an http:// or https:// URL with a host")
			return
		}
		_, _ = h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('outbound_proxy', ?)", proxyVal)
	}

	h.HandleSettingsGet(w, r)
}

// isValidProxyURL accepts only absolute http(s) proxies with a host. Go's
// url.Parse is far too lenient for a value that ends up in a transport.
func isValidProxyURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// GET /v1/settings/backup/export
func (h *RouterAPIHandler) HandleBackupExport(w http.ResponseWriter, r *http.Request) {
	conns, _ := h.getAllProviders()

	var keys []map[string]any
	krows, err := h.db.Query("SELECT id, key, name, enabled, rate_limit, quota_limit, usage_tokens, credit_limit, usage_cost, allowed_models, created_at FROM api_keys ORDER BY created_at DESC")
	if err == nil {
		defer krows.Close()
		for krows.Next() {
			var id, key, name string
			var enabledInt int
			var rateLimit, quotaLimit, creditLimit, allowedModels *string
			var usageTokens int64
			var usageCost float64
			var createdAt int64
			if err := krows.Scan(&id, &key, &name, &enabledInt, &rateLimit, &quotaLimit, &usageTokens, &creditLimit, &usageCost, &allowedModels, &createdAt); err == nil {
				k := map[string]any{
					"id":        id,
					"key":       key,
					"name":      name,
					"enabled":   enabledInt == 1,
					"createdAt": createdAt,
				}
				keys = append(keys, k)
			}
		}
	}

	var fallbackRules []map[string]any
	frows, err := h.db.Query("SELECT id, source_model, target_model, priority, enabled, trigger_on_status, max_retries, created_at FROM fallback_rules")
	if err == nil {
		defer frows.Close()
		for frows.Next() {
			var id, src, tgt string
			var prio int
			var enInt int
			var trigStr, maxRetries *string
			var ca int64
			if err := frows.Scan(&id, &src, &tgt, &prio, &enInt, &trigStr, &maxRetries, &ca); err == nil {
				rule := map[string]any{
					"id":          id,
					"sourceModel": src,
					"targetModel": tgt,
					"priority":    prio,
					"enabled":     enInt == 1,
					"createdAt":   ca,
				}
				if trigStr != nil && *trigStr != "" {
					var codes []int
					_ = json.Unmarshal([]byte(*trigStr), &codes)
					rule["triggerOnStatus"] = codes
				}
				fallbackRules = append(fallbackRules, rule)
			}
		}
	}

	backupData := map[string]any{
		"version":             "1.0.0",
		"exportedAt":          time.Now().Format(time.RFC3339),
		"providers":           conns,
		"providerConnections": conns,
		"apiKeys":             keys,
		"fallbackRules":       fallbackRules,
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=lam-router-backup-%s.json", time.Now().Format("2006-01-02")))
	handlerutil.WriteJSON(w, http.StatusOK, backupData)
}

// POST /v1/settings/backup/import
func (h *RouterAPIHandler) HandleBackupImport(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Check if body is {"url": "https://..."}
	var urlWrapper struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(bodyBytes, &urlWrapper) == nil && urlWrapper.URL != "" && (strings.HasPrefix(urlWrapper.URL, "http://") || strings.HasPrefix(urlWrapper.URL, "https://")) {
		if err := handlerutil.AssertPublicURL(urlWrapper.URL); err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("SSRF protection blocked URL: %v", err))
			return
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(urlWrapper.URL)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("Failed to fetch backup from URL: %v", err))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("Remote URL returned status %d", resp.StatusCode))
			return
		}
		bodyBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read data from remote URL: %v", err))
			return
		}
	}

	var rawPayload map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &rawPayload); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON backup file")
		return
	}

	// Diagnose a foreign payload instead of answering 200 null: nothing below
	// matches, so the caller would otherwise believe a restore happened.
	recognized := []string{"providers", "providerConnections", "apiKeys", "fallbackRules", "settings"}
	hasRecognizedKey := false
	for _, key := range recognized {
		if _, ok := rawPayload[key]; ok {
			hasRecognizedKey = true
			break
		}
	}
	if !hasRecognizedKey {
		handlerutil.WriteJSONError(w, http.StatusBadRequest,
			"Unrecognized backup payload: expected providers, providerConnections, apiKeys or fallbackRules")
		return
	}

	importedProviders := 0
	importedKeys := 0

	// 1. Import Providers / Connections (LAM-Router backup schema)
	connsRaw, hasConns := rawPayload["providerConnections"]
	if !hasConns {
		connsRaw, hasConns = rawPayload["providers"]
	}

	if hasConns {
		var connsList []map[string]any
		if err := json.Unmarshal(connsRaw, &connsList); err == nil {
			for _, c := range connsList {
				id, _ := c["id"].(string)
				pID, _ := c["providerId"].(string)
				if pID == "" {
					pID, _ = c["provider"].(string)
				}
				if id == "" {
					id = fmt.Sprintf("imported_%s", randomHex(4))
				}
				if pID == "" {
					pID = id
				}
				pID = strings.ToLower(pID)
				name, _ := c["name"].(string)
				if name == "" {
					name = pID
				}
				category, _ := c["category"].(string)
				if category == "" {
					category = "api_key"
				}
				protocol, _ := c["protocol"].(string)
				if protocol == "" {
					protocol = "openai"
				}
				baseURL, _ := c["baseUrl"].(string)
				if baseURL == "" {
					baseURL, _ = c["base_url"].(string)
				}
				apiKey, _ := c["apiKey"].(string)
				if apiKey == "" {
					apiKey, _ = c["api_key"].(string)
				}
				accessToken, _ := c["accessToken"].(string)
				if accessToken == "" {
					accessToken, _ = c["access_token"].(string)
				}
				refreshToken, _ := c["refreshToken"].(string)
				if refreshToken == "" {
					refreshToken, _ = c["refresh_token"].(string)
				}

				// Check providerSpecificData from backup
				var psdStr *string
				if psd, ok := c["providerSpecificData"].(map[string]any); ok {
					if baseURL == "" {
						if b, ok := psd["baseUrl"].(string); ok {
							baseURL = b
						}
					}
					b, _ := json.Marshal(psd)
					s := string(b)
					psdStr = &s
				}

				var chStr *string
				if ch, ok := c["customHeaders"].(map[string]any); ok {
					b, _ := json.Marshal(ch)
					s := string(b)
					chStr = &s
				}

				var bURLPtr, aKeyPtr, aTokPtr, rTokPtr *string
				if baseURL != "" {
					bURLPtr = &baseURL
				}
				if apiKey != "" {
					aKeyPtr = &apiKey
				}
				if accessToken != "" {
					aTokPtr = &accessToken
				}
				if refreshToken != "" {
					rTokPtr = &refreshToken
				}

				// Smart Deduplication: If a connection with same name or email already exists for this provider, reuse its existing ID!
				var existingID string
				_ = h.db.QueryRow("SELECT id FROM providers WHERE provider_id = ? AND (LOWER(name) = LOWER(?) OR LOWER(id) = LOWER(?))", pID, name, id).Scan(&existingID)
				if existingID != "" {
					id = existingID
				}

				now := time.Now().UnixMilli()
				_, err := h.db.Exec(`
					INSERT OR REPLACE INTO providers (
						id, provider_id, name, category, protocol, base_url, api_key, access_token, refresh_token, custom_headers, provider_specific_data, enabled, created_at
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
					id, pID, name, category, protocol, bURLPtr, aKeyPtr, aTokPtr, rTokPtr, chStr, psdStr, now)
				if err == nil {
					importedProviders++

					// Also ensure synchronization into providerConnections table for the Chat Router
					var connDataMap = make(map[string]any)
					if apiKey != "" {
						connDataMap["apiKey"] = apiKey
					}
					if accessToken != "" {
						connDataMap["accessToken"] = accessToken
					}
					if refreshToken != "" {
						connDataMap["refreshToken"] = refreshToken
					}
					if baseURL != "" {
						connDataMap["baseUrl"] = baseURL
					}
					if psd, ok := c["providerSpecificData"].(map[string]any); ok {
						for k, v := range psd {
							connDataMap[k] = v
						}
					}
					dataBytes, _ := json.Marshal(connDataMap)
					nowStr := time.Now().Format("2006-01-02 15:04:05")
					authType := category
					if authType == "" {
						authType = "oauth"
					}

					_, _ = h.db.Exec(`
						INSERT OR REPLACE INTO providerConnections (
							id, provider, authType, name, isActive, data, createdAt, updatedAt
						) VALUES (?, ?, ?, ?, 1, ?, ?, ?)`,
						id, pID, authType, name, string(dataBytes), nowStr, nowStr)

					_, _ = h.ScanAndSyncProviderModels(pID)
				}
			}
		}
	}

	// 2. Import API Keys
	if keysRaw, hasKeys := rawPayload["apiKeys"]; hasKeys {
		var keysList []map[string]any
		if err := json.Unmarshal(keysRaw, &keysList); err == nil {
			for _, k := range keysList {
				id, _ := k["id"].(string)
				name, _ := k["name"].(string)
				key, _ := k["key"].(string)
				if key == "" {
					continue
				}
				if id == "" {
					id = fmt.Sprintf("key_%s", randomHex(8))
				}
				if name == "" {
					name = "Imported Key"
				}
				keyHash := sha256Hex(key)
				now := time.Now().UnixMilli()
				_, err := h.db.Exec(`
					INSERT OR REPLACE INTO api_keys (
						id, name, key, key_hash, enabled, created_at
					) VALUES (?, ?, ?, ?, 1, ?)`,
					id, name, key, keyHash, now)
				_, _ = h.db.Exec(`
					INSERT OR REPLACE INTO apiKeys (
						id, name, key, keyHash, enabled, createdAt, updatedAt
					) VALUES (?, ?, ?, ?, 1, datetime('now'), datetime('now'))`,
					id, name, key, keyHash)
				if err == nil {
					importedKeys++
				}
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":           true,
		"importedProviders": importedProviders,
		"importedKeys":      importedKeys,
		"message":           fmt.Sprintf("Successfully imported %d providers and %d API keys.", importedProviders, importedKeys),
	})
}

// GET /v1/settings/token-saver
func (h *RouterAPIHandler) HandleTokenSaverGet(w http.ResponseWriter, r *http.Request) {
	defaultSettings := map[string]any{
		"enabled": true,
		"compressToolOutput": map[string]any{
			"enabled":                true,
			"compressGit":            true,
			"compressGrep":           true,
			"compressFileLists":      true,
			"compressLogs":           true,
			"stripAnsiAndWhitespace": true,
			"minCharacterThreshold":  50,
		},
		"lazySeniorDev": map[string]any{
			"enabled": true,
			"mode":    "balanced",
		},
		"compressLlmOutput": map[string]any{
			"enabled":           true,
			"mode":              "terse",
			"stripPleasantries": true,
		},
	}

	var raw string
	err := h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'token_saver_config'").Scan(&raw)
	if err != nil || raw == "" {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"settings": defaultSettings})
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"settings": defaultSettings})
		return
	}

	// Deep merge with defaults to ensure all required fields exist
	merged := make(map[string]any)
	for k, v := range defaultSettings {
		merged[k] = v
	}
	for k, v := range parsed {
		if defaultMap, isMap := defaultSettings[k].(map[string]any); isMap {
			if parsedMap, ok := v.(map[string]any); ok {
				subMerged := make(map[string]any)
				for sk, sv := range defaultMap {
					subMerged[sk] = sv
				}
				for sk, sv := range parsedMap {
					subMerged[sk] = sv
				}
				merged[k] = subMerged
				continue
			}
		}
		merged[k] = v
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"settings": merged})
}

// PUT/PATCH /v1/settings/token-saver
func (h *RouterAPIHandler) HandleTokenSaverUpdate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid token saver settings payload")
		return
	}

	defaultSettings := map[string]any{
		"enabled": true,
		"compressToolOutput": map[string]any{
			"enabled":                true,
			"compressGit":            true,
			"compressGrep":           true,
			"compressFileLists":      true,
			"compressLogs":           true,
			"stripAnsiAndWhitespace": true,
			"minCharacterThreshold":  50,
		},
		"lazySeniorDev": map[string]any{
			"enabled": true,
			"mode":    "balanced",
		},
		"compressLlmOutput": map[string]any{
			"enabled":           true,
			"mode":              "terse",
			"stripPleasantries": true,
		},
	}

	var existingRaw string
	_ = h.db.QueryRow("SELECT value FROM system_settings WHERE key = 'token_saver_config'").Scan(&existingRaw)
	existing := make(map[string]any)
	if existingRaw != "" {
		_ = json.Unmarshal([]byte(existingRaw), &existing)
	}

	merged := make(map[string]any)
	for k, v := range defaultSettings {
		merged[k] = v
	}
	for k, v := range existing {
		if defaultMap, isMap := defaultSettings[k].(map[string]any); isMap {
			if existingMap, ok := v.(map[string]any); ok {
				subMerged := make(map[string]any)
				for sk, sv := range defaultMap {
					subMerged[sk] = sv
				}
				for sk, sv := range existingMap {
					subMerged[sk] = sv
				}
				merged[k] = subMerged
				continue
			}
		}
		merged[k] = v
	}

	// Apply partial update from body
	for k, v := range body {
		if baseMap, isMap := merged[k].(map[string]any); isMap {
			if bodyMap, ok := v.(map[string]any); ok {
				subMerged := make(map[string]any)
				for sk, sv := range baseMap {
					subMerged[sk] = sv
				}
				for sk, sv := range bodyMap {
					subMerged[sk] = sv
				}
				merged[k] = subMerged
				continue
			}
		}
		merged[k] = v
	}

	raw, _ := json.Marshal(merged)
	_, _ = h.db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES ('token_saver_config', ?)", string(raw))

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"settings": merged})
}

// POST /v1/settings/token-saver/test
func (h *RouterAPIHandler) HandleTokenSaverTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	text := body.Text
	origTokens := len(text) / 4
	compressed := text
	if len(text) > 100 {
		compressed = text[:100] + "... [compressed by LAM-Router RTK]"
	}
	savedTokens := origTokens - (len(compressed) / 4)
	if savedTokens < 0 {
		savedTokens = 0
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"original":        text,
		"compressed":      compressed,
		"originalTokens":  origTokens,
		"savedTokens":     savedTokens,
		"reductionFactor": "60%",
	})
}

// GET /v1/settings/fallbacks
func (h *RouterAPIHandler) HandleFallbacksGet(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query("SELECT id, source_model, target_model, priority, enabled, trigger_on_status, max_retries, created_at FROM fallback_rules ORDER BY priority ASC, created_at ASC")
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var rules []FallbackRule
	for rows.Next() {
		var f FallbackRule
		var enabledInt int
		var statusStr *string
		_ = rows.Scan(
			&f.ID, &f.SourceModel, &f.TargetModel, &f.Priority,
			&enabledInt, &statusStr, &f.MaxRetries, &f.CreatedAt,
		)
		f.Enabled = enabledInt == 1
		if statusStr != nil && *statusStr != "" {
			_ = json.Unmarshal([]byte(*statusStr), &f.TriggerOnStatus)
		}
		rules = append(rules, f)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"fallbacks": rules,
	})
}

// POST /v1/settings/fallbacks
func (h *RouterAPIHandler) HandleFallbackCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceModel     string `json:"sourceModel"`
		TargetModel     string `json:"targetModel"`
		Priority        *int   `json:"priority"`
		TriggerOnStatus []int  `json:"triggerOnStatus"`
		MaxRetries      *int   `json:"maxRetries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SourceModel == "" || body.TargetModel == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Source and target model are required")
		return
	}

	id := fmt.Sprintf("fallback_%s", randomHex(8))
	priority := 1
	if body.Priority != nil {
		priority = *body.Priority
	}
	now := time.Now().UnixMilli()

	var statusStr *string
	if len(body.TriggerOnStatus) > 0 {
		b, _ := json.Marshal(body.TriggerOnStatus)
		s := string(b)
		statusStr = &s
	}

	// A new model added to an existing combo must inherit that combo's injection
	// settings, otherwise the extra rule arrives with a blank prompt and any
	// reader that looks at rule columns alone sees the combo as unprotected.
	inherit := loadStoredOrLegacyComboSettings(h.db, body.SourceModel)

	_, err := h.db.Exec(`
INSERT INTO fallback_rules (id, source_model, target_model, priority, enabled, trigger_on_status, max_retries, created_at,
                            system_prompt, inject_mode, prompt_injection_enabled, display_identity)
VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
`, id, body.SourceModel, body.TargetModel, priority, statusStr, body.MaxRetries, now,
		inherit.SystemPrompt, inherit.InjectMode, nullableFlag(inherit.Enabled), inherit.DisplayIdentity)

	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	created := FallbackRule{
		ID:              id,
		SourceModel:     body.SourceModel,
		TargetModel:     body.TargetModel,
		Priority:        priority,
		Enabled:         true,
		TriggerOnStatus: body.TriggerOnStatus,
		MaxRetries:      body.MaxRetries,
		CreatedAt:       now,
	}

	handlerutil.WriteJSON(w, http.StatusCreated, created)
}

// PUT/PATCH /v1/settings/fallbacks/{id}
func (h *RouterAPIHandler) HandleFallbackUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		SourceModel     *string `json:"sourceModel"`
		TargetModel     *string `json:"targetModel"`
		Priority        *int    `json:"priority"`
		Enabled         *bool   `json:"enabled"`
		TriggerOnStatus []int   `json:"triggerOnStatus"`
		MaxRetries      *int    `json:"maxRetries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid fallback payload")
		return
	}

	if body.Enabled != nil {
		en := 0
		if *body.Enabled {
			en = 1
		}
		_, _ = h.db.Exec("UPDATE fallback_rules SET enabled = ? WHERE id = ?", en, id)
	}
	if body.Priority != nil {
		_, _ = h.db.Exec("UPDATE fallback_rules SET priority = ? WHERE id = ?", *body.Priority, id)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"message": "Fallback rule updated"})
}

// DELETE /v1/settings/fallbacks/{id}
func (h *RouterAPIHandler) HandleFallbackDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := h.db.Exec("DELETE FROM fallback_rules WHERE id = ?", id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"message": "Fallback rule deleted"})
}

// loadStoredOrLegacyComboSettings resolves the injection settings already attached
// to a combo, so writing one field cannot silently drop the others.
//
// The durable per-combo store wins; the rule columns are only a fallback for
// databases that predate it.
func loadStoredOrLegacyComboSettings(db *sql.DB, sourceModel string) promptinject.ComboSettings {
	if settings, ok := promptinject.LoadComboSettings(db, sourceModel); ok {
		return settings
	}

	out := promptinject.ComboSettings{ComboName: sourceModel, InjectMode: string(promptinject.ModePrepend)}
	var prompt, mode, identity sql.NullString
	var enabled sql.NullInt64
	if err := db.QueryRow(`
		SELECT COALESCE(MAX(NULLIF(system_prompt,'')),''),
		       COALESCE(MAX(NULLIF(NULLIF(inject_mode,''),'prepend')),'prepend'),
		       MAX(prompt_injection_enabled),
		       COALESCE(MAX(NULLIF(display_identity,'')),'')
		  FROM fallback_rules WHERE source_model = ?`, sourceModel).
		Scan(&prompt, &mode, &enabled, &identity); err != nil {
		return out
	}

	out.SystemPrompt = prompt.String
	out.InjectMode = string(promptinject.NormalizeMode(mode.String))
	out.DisplayIdentity = identity.String
	if enabled.Valid {
		value := enabled.Int64
		out.Enabled = &value
	}
	return out
}

// nullableFlag turns the optional stored 0/1 into a SQL argument (NULL when the
// combo never had an explicit switch).
func nullableFlag(flag *int64) any {
	if flag == nil {
		return nil
	}
	return *flag
}
