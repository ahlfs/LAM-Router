package srouterapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"9router/proxy/internal/handlerutil"
)

type DBAPIKey struct {
	ID            string   `json:"id"`
	Key           string   `json:"key"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	RateLimit     int      `json:"rateLimit"`
	QuotaLimit    int      `json:"quotaLimit"`
	UsageTokens   int      `json:"usageTokens"`
	CreditLimit   float64  `json:"creditLimit"`
	UsageCost     float64  `json:"usageCost"`
	AllowedModels []string `json:"allowed_models"`
	CreatedAt     int64    `json:"createdAt"`
}

// GET /v1/keys
func (h *SRouterHandler) HandleKeysList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query("SELECT id, key, name, enabled, rate_limit, quota_limit, usage_tokens, credit_limit, usage_cost, allowed_models, created_at FROM api_keys ORDER BY created_at DESC")
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var keys []DBAPIKey
	for rows.Next() {
		var k DBAPIKey
		var enabledInt int
		var modelsStr *string
		err := rows.Scan(
			&k.ID, &k.Key, &k.Name, &enabledInt, &k.RateLimit,
			&k.QuotaLimit, &k.UsageTokens, &k.CreditLimit,
			&k.UsageCost, &modelsStr, &k.CreatedAt,
		)
		if err != nil {
			continue
		}
		k.Enabled = enabledInt == 1
		if modelsStr != nil && *modelsStr != "" {
			_ = json.Unmarshal([]byte(*modelsStr), &k.AllowedModels)
		}
		keys = append(keys, k)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   keys,
	})
}

// POST /v1/keys
func (h *SRouterHandler) HandleKeyCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string   `json:"name"`
		RateLimit     *int     `json:"rateLimit"`
		QuotaLimit    *int     `json:"quotaLimit"`
		CreditLimit   *float64 `json:"creditLimit"`
		AllowedModels []string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Key name is required")
		return
	}

	id := fmt.Sprintf("key_%s", randomHex(8))
	key := fmt.Sprintf("lam-live-%s", randomHex(16))
	now := time.Now().UnixMilli()

	rateLimit := 0
	if body.RateLimit != nil {
		rateLimit = *body.RateLimit
	}
	quotaLimit := 0
	if body.QuotaLimit != nil {
		quotaLimit = *body.QuotaLimit
	}
	creditLimit := 0.0
	if body.CreditLimit != nil {
		creditLimit = *body.CreditLimit
	}

	var modelsStr *string
	if len(body.AllowedModels) > 0 {
		b, _ := json.Marshal(body.AllowedModels)
		s := string(b)
		modelsStr = &s
	}

	_, err := h.db.Exec(`
INSERT INTO api_keys (id, key, name, enabled, rate_limit, quota_limit, usage_tokens, credit_limit, usage_cost, allowed_models, created_at)
VALUES (?, ?, ?, 1, ?, ?, 0, ?, 0, ?, ?)
`, id, key, body.Name, rateLimit, quotaLimit, creditLimit, modelsStr, now)

	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Also insert into 9router apiKeys table so it works seamlessly for chat endpoints
	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, _ = h.db.Exec(`
INSERT INTO apiKeys (id, key, name, isActive, createdAt)
VALUES (?, ?, ?, 1, ?)
`, id, key, body.Name, nowISO)

	created := DBAPIKey{
		ID:            id,
		Key:           key,
		Name:          body.Name,
		Enabled:       true,
		RateLimit:     rateLimit,
		QuotaLimit:    quotaLimit,
		UsageTokens:   0,
		CreditLimit:   creditLimit,
		UsageCost:     0,
		AllowedModels: body.AllowedModels,
		CreatedAt:     now,
	}

	handlerutil.WriteJSON(w, http.StatusCreated, created)
}

// POST /v1/keys/{id}/credit
func (h *SRouterHandler) HandleKeyAddCredit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid credit payload")
		return
	}

	res, err := h.db.Exec("UPDATE api_keys SET credit_limit = credit_limit + ? WHERE id = ?", body.Amount, id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		handlerutil.WriteJSONError(w, http.StatusNotFound, fmt.Sprintf("Key '%s' not found", id))
		return
	}

	var k DBAPIKey
	var enabledInt int
	var modelsStr *string
	_ = h.db.QueryRow("SELECT id, key, name, enabled, rate_limit, quota_limit, usage_tokens, credit_limit, usage_cost, allowed_models, created_at FROM api_keys WHERE id = ?", id).Scan(
		&k.ID, &k.Key, &k.Name, &enabledInt, &k.RateLimit,
		&k.QuotaLimit, &k.UsageTokens, &k.CreditLimit,
		&k.UsageCost, &modelsStr, &k.CreatedAt,
	)
	k.Enabled = enabledInt == 1
	if modelsStr != nil && *modelsStr != "" {
		_ = json.Unmarshal([]byte(*modelsStr), &k.AllowedModels)
	}

	handlerutil.WriteJSON(w, http.StatusOK, k)
}

// DELETE /v1/keys/{id}
func (h *SRouterHandler) HandleKeyDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := h.db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		handlerutil.WriteJSONError(w, http.StatusNotFound, fmt.Sprintf("Key '%s' not found", id))
		return
	}

	_, _ = h.db.Exec("DELETE FROM apiKeys WHERE id = ?", id)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"message": "API Key revoked and deleted successfully"})
}
