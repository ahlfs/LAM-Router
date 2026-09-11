package middleware

import (
	"context"
	"9router/proxy/internal/log"
	"net/http"
	"strings"

	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/models"
)

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
			if cookie, err := r.Cookie("srouter_admin_session"); err == nil && cookie != nil && cookie.Value != "" {
				if repo.VerifyAdminSession(cookie.Value) {
					adminKeyName := "Admin Dashboard Session"
					adminKeyObj := &models.APIKey{
						ID:       "admin_session",
						Key:      "admin",
						Name:     &adminKeyName,
						IsActive: 1,
					}
					ctx := context.WithValue(r.Context(), ApiKeyContextKey, adminKeyObj)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			apiKeyString := ExtractApiKey(r)
			if apiKeyString == "" {
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Authentication required. Provide an API key via Authorization: Bearer *** header or ?key=<key> query parameter.")
				return
			}

			// Validate via SQLite repository and retrieve details
			apiKeyObj, err := repo.GetApiKeyByKey(apiKeyString)
			if err != nil {
				log.Error("auth", "DB lookup error", "error", err)
				handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
			if apiKeyObj == nil {
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid API key.")
				return
			}

			if apiKeyObj.IsActive != 1 {
				handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid or inactive API key.")
				return
			}

			// Check token quota limit
			if apiKeyObj.QuotaLimit > 0 && apiKeyObj.UsageTokens >= apiKeyObj.QuotaLimit {
				log.Warn("auth", "token quota limit exceeded", "key", apiKeyObj.Key, "usage", apiKeyObj.UsageTokens, "limit", apiKeyObj.QuotaLimit)
				handlerutil.WriteJSONError(w, http.StatusTooManyRequests, "Token quota limit exceeded. Please top up or increase the quota limit for this API key.")
				return
			}

			// Check credit cost limit
			if apiKeyObj.CreditLimit > 0 && apiKeyObj.UsageCost >= apiKeyObj.CreditLimit {
				log.Warn("auth", "credit limit exceeded", "key", apiKeyObj.Key, "cost", apiKeyObj.UsageCost, "limit", apiKeyObj.CreditLimit)
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
