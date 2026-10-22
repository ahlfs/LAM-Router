package routerapi

import (
	"encoding/json"
	"net/http"

	"lamrouter/internal/gatewayprefs"
	"lamrouter/internal/handlerutil"
)

// GET /v1/settings/preferences
//
// These are the Settings fields the gateway actually reads: log retention,
// logging detail and token accounting. Everything else in the dashboard is a
// browser preference and is deliberately absent here.
func (h *RouterAPIHandler) HandlePreferencesGet(w http.ResponseWriter, r *http.Request) {
	prefs := gatewayprefs.Load(h.db)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"preferences": prefs,
		"defaults":    gatewayprefs.Defaults(),
	})
}

// POST /v1/settings/preferences
//
// Accepts a partial patch; only the supplied keys change. Values are validated
// before they are stored, so a bad window or an unknown logging level is a 400
// instead of a silently ignored write.
func (h *RouterAPIHandler) HandlePreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	var patch gatewayprefs.Patch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid preferences payload")
		return
	}
	if patch.LogRetentionDays == nil && patch.LoggingLevel == nil && patch.RecordTokenUsage == nil &&
		patch.DefaultTemperature == nil && patch.DefaultTopP == nil && patch.DefaultMaxTokens == nil &&
		patch.DefaultSystemPrompt == nil && patch.StreamResponse == nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "No recognised preference supplied")
		return
	}

	prefs, err := gatewayprefs.Apply(h.db, patch)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"preferences": prefs,
	})
}
