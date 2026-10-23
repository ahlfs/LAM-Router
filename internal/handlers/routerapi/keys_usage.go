package routerapi

import (
	"encoding/json"
	"net/http"
	"time"

	"lamrouter/internal/db"
	"lamrouter/internal/handlerutil"
)

// GET /v1/keys/usage
//
// Per-key API usage for the Endpoint & Keys page: last model, last used time,
// lifetime tokens/cost and the per-model breakdown. Everything comes from the
// api_key_model_usage rollup plus the counters on api_keys, so the cost of this
// call is bounded by the number of keys (and models per key), not by how many
// requests the gateway has served.
func (h *RouterAPIHandler) HandleKeysUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repo := db.NewRepo(h.db)
	keys, err := repo.KeyUsageSummary(5)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"keys":        keys,
		"generatedAt": time.Now().UnixMilli(),
	})
}

// POST /v1/keys/usage/recount
//
// Rebuilds the lifetime token/cost counters from the per-model rollup. Body:
// {"id":"key_..."} to recount one key, or an empty body to recount every key.
// Explicit only: a manual reset is never overwritten by routine traffic.
func (h *RouterAPIHandler) HandleKeysUsageRecount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		ID string `json:"id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // an empty body recounts everything
	}

	recounted, err := db.NewRepo(h.db).RecountApiKeyUsage(body.ID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"count":     len(recounted),
		"recounted": recounted,
	})
}
