package middleware

import (
	"net/http"
	"strings"

	"lamrouter/internal/db"
	"lamrouter/internal/handlerutil"
)

// RequireAdminSession creates a middleware that protects administrative routes.
// It accepts either:
// 1. A valid `lamrouter_admin_session` cookie verified against the database.
// 2. An `Authorization: Bearer <token>` matching an active admin session or system secret.
// 3. An `X-Admin-Token: <token>` header as a secondary fallback for CLI/TUI tools.
func RequireAdminSession(repo *db.Repo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Check session cookie
			if cookie, err := r.Cookie("lamrouter_admin_session"); err == nil && cookie != nil && cookie.Value != "" {
				if repo.VerifyAdminSession(cookie.Value) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// 2. Check Authorization: Bearer <token>
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
				if token != "" && repo.VerifyAdminSession(token) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// 3. Fallback: X-Admin-Token header
			if token := strings.TrimSpace(r.Header.Get("X-Admin-Token")); token != "" {
				if repo.VerifyAdminSession(token) {
					next.ServeHTTP(w, r)
					return
				}
			}

			handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Admin authentication required. Please log in to access this endpoint.")
		})
	}
}
