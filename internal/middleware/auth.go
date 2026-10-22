package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"lamrouter/internal/db"
	"lamrouter/internal/handlerutil"
	"lamrouter/internal/log"
	"lamrouter/internal/models"
)

// unknownKeyFingerprint derives a short, non-reversible tag for a presented key
// that matched no api_keys row. Repeated refusals from the same key stay
// correlatable in the audit trail without the credential being stored.
func unknownKeyFingerprint(apiKey string) string {
	if apiKey == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(apiKey))
	return "unmatched:" + hex.EncodeToString(sum[:])[:8]
}

// apiKeyDisplayName returns the human label for a key, tolerating a NULL name.
func apiKeyDisplayName(apiKeyObj *models.APIKey) string {
	if apiKeyObj == nil || apiKeyObj.Name == nil {
		return ""
	}
	return *apiKeyObj.Name
}

// authLogSeq disambiguates audit rows written within the same millisecond.
var authLogSeq atomic.Uint64

// logAuthError records a refused request. apiKeyID is the api_keys.id when the
// key resolved, otherwise a fingerprint; history rows written before this
// existed hold raw keys, so callers must never pass one through here.
func logAuthError(repo *db.Repo, apiKeyID, apiKeyName, errMsg string, statusCode int) {
	if repo == nil {
		return
	}
	rawDB := repo.RawDB()
	if rawDB == nil {
		return
	}
	nowMillis := time.Now().UnixMilli()
	// Two refusals can land in the same millisecond; a shared id would let
	// INSERT OR REPLACE overwrite the first audit row with the second.
	reqLogID := fmt.Sprintf("req_%d_%d_auth", nowMillis, authLogSeq.Add(1))
	if apiKeyID == "" {
		apiKeyID = "unknown"
	}
	// provider_id names the stage that refused the request, but there is no
	// model to record: no routing happened. Filling the model column with a
	// placeholder is what made the dashboard list "Auth" as a model, so it stays
	// empty and the read paths keep older rows that stored "auth" in both
	// columns out of the model and provider breakdowns.
	_, _ = rawDB.Exec(`
		INSERT OR REPLACE INTO request_logs (
			id, api_key_id, api_key_name, provider_id, model, prompt_tokens, completion_tokens, total_tokens,
			status_code, latency_ms, created_at, cached_tokens, cache_creation_tokens, compression_tokens, reasoning_tokens, estimated_cost,
			fallback_occurred, fallback_reason
		) VALUES (?, ?, ?, 'auth', '', 0, 0, 0, ?, 0, ?, 0, 0, 0, 0, 0, 0, ?)
	`, reqLogID, apiKeyID, apiKeyName, statusCode, nowMillis, errMsg)
}

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

// ApiKeyContextKey is the context key for the authenticated API key object.
const ApiKeyContextKey ContextKey = "apiKey"

// RequireApiKey creates a middleware handler that authenticates requests using client API keys.
// It checks the Authorization header (Bearer <key>) and the query parameter `key`.
// Valid keys are retrieved from the SQLite database; inactive or disabled keys are rejected with 401.
func RequireApiKey(repo *db.Repo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Allow authenticated admin sessions from Web Dashboard (Playground / Benchmark)
			if cookie, err := r.Cookie("lamrouter_admin_session"); err == nil && cookie != nil && cookie.Value != "" {
				if repo.VerifyAdminSession(cookie.Value) {
					adminKeyName := models.AdminSessionKeyName
					adminKeyObj := &models.APIKey{
						ID:       models.AdminSessionKeyID,
						Key:      "admin",
						Name:     &adminKeyName,
						IsActive: 1,
					}
					ctx := context.WithValue(r.Context(), ApiKeyContextKey, adminKeyObj)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Open access mode: the operator disabled API key enforcement. Requests
			// pass through, and a valid key is still attributed when one is offered.
			if !repo.APIKeyRequired() {
				if provided := ExtractApiKey(r); provided != "" {
					if obj, err := repo.GetApiKeyByKey(provided); err == nil && obj != nil && obj.IsActive == 1 {
						ctx := context.WithValue(r.Context(), ApiKeyContextKey, obj)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				next.ServeHTTP(w, r)
				return
			}

			apiKeyString := ExtractApiKey(r)
			if apiKeyString == "" {
				logAuthError(repo, "none", "", "Authentication required", http.StatusUnauthorized)
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Authentication required. Provide an API key via Authorization: Bearer <key> or X-API-Key header.")
				return
			}

			// Validate via SQLite repository and retrieve details
			apiKeyObj, err := repo.GetApiKeyByKey(apiKeyString)
			if err != nil {
				log.Error("auth", "DB lookup error", "error", err)
				logAuthError(repo, unknownKeyFingerprint(apiKeyString), "", "DB lookup error: "+err.Error(), http.StatusInternalServerError)
				handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
			if apiKeyObj == nil {
				logAuthError(repo, unknownKeyFingerprint(apiKeyString), "", "Invalid API key", http.StatusUnauthorized)
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid API key.")
				return
			}

			if apiKeyObj.IsActive != 1 {
				logAuthError(repo, apiKeyObj.ID, apiKeyDisplayName(apiKeyObj), "Invalid or inactive API key", http.StatusUnauthorized)
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid or inactive API key.")
				return
			}

			// Check token quota limit
			if apiKeyObj.QuotaLimit > 0 && apiKeyObj.UsageTokens >= apiKeyObj.QuotaLimit {
				log.Warn("auth", "token quota limit exceeded", "key", apiKeyObj.ID, "usage", apiKeyObj.UsageTokens, "limit", apiKeyObj.QuotaLimit)
				logAuthError(repo, apiKeyObj.ID, apiKeyDisplayName(apiKeyObj), "Token quota limit exceeded", http.StatusTooManyRequests)
				handlerutil.WriteJSONError(w, http.StatusTooManyRequests, "Token quota limit exceeded. Please top up or increase the quota limit for this API key.")
				return
			}

			// Check credit cost limit
			if apiKeyObj.CreditLimit > 0 && apiKeyObj.UsageCost >= apiKeyObj.CreditLimit {
				log.Warn("auth", "credit limit exceeded", "key", apiKeyObj.ID, "cost", apiKeyObj.UsageCost, "limit", apiKeyObj.CreditLimit)
				logAuthError(repo, apiKeyObj.ID, apiKeyDisplayName(apiKeyObj), "Credit limit exceeded", http.StatusTooManyRequests)
				handlerutil.WriteJSONError(w, http.StatusTooManyRequests, "Credit limit exceeded. Please add credit to continue using this API key.")
				return
			}

			// Inject API Key info into the request context for downstream handlers/logging
			ctx := context.WithValue(r.Context(), ApiKeyContextKey, apiKeyObj)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAuthenticatedApiKey retrieves the authenticated APIKey object from the request context.
func GetAuthenticatedApiKey(r *http.Request) *models.APIKey {
	val := r.Context().Value(ApiKeyContextKey)
	if val == nil {
		return nil
	}
	keyObj, ok := val.(*models.APIKey)
	if !ok {
		return nil
	}
	return keyObj
}

// ExtractApiKey extracts the client API key from the request.
// Only header-based auth is accepted — keys in query strings would leak via
// browser history, referrers, and upstream proxy logs.
func ExtractApiKey(r *http.Request) string {
	// 1. Try Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return strings.TrimSpace(parts[1])
		}
	}

	// 2. Try custom X-API-Key header as fallback
	if xApiKey := r.Header.Get("X-API-Key"); xApiKey != "" {
		return xApiKey
	}

	return ""
}
