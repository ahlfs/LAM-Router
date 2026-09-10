package srouterapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"9router/proxy/internal/handlerutil"
)

const (
	AdminSessionCookie = "srouter_admin_session"
	AdminSessionTTL    = 7 * 24 * time.Hour
)

// HashPassword hashes a password with sha256 + salt for simple, robust local verification.
func HashPassword(password string) string {
	salt := "lam-router-salt"
	h := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(h[:])
}

// Check if admin account is configured
func (h *SRouterHandler) hasAdminAccount() bool {
	var count int
	err := h.db.QueryRow("SELECT COUNT(*) FROM admin_account WHERE id = 1").Scan(&count)
	return err == nil && count > 0
}

// Get admin password hash
func (h *SRouterHandler) getAdminPasswordHash() (string, error) {
	var hash string
	err := h.db.QueryRow("SELECT password_hash FROM admin_account WHERE id = 1").Scan(&hash)
	return hash, err
}

// Verify session from cookie or header
func (h *SRouterHandler) verifyAdminSession(r *http.Request) bool {
	var token string
	cookie, err := r.Cookie(AdminSessionCookie)
	if err == nil && cookie != nil && cookie.Value != "" {
		token = cookie.Value
	} else {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if token == "" {
		return false
	}

	tokenHash := sha256Hex(token)
	now := time.Now().UnixMilli()

	// Clean expired sessions
	_, _ = h.db.Exec("DELETE FROM admin_sessions WHERE expires_at <= ?", now)

	var count int
	err = h.db.QueryRow("SELECT COUNT(*) FROM admin_sessions WHERE token_hash = ? AND expires_at > ?", tokenHash, now).Scan(&count)
	return err == nil && count > 0
}

// Create a new admin session and set cookie
func (h *SRouterHandler) createAdminSession(w http.ResponseWriter) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	tokenHash := sha256Hex(token)

	now := time.Now().UnixMilli()
	expiresAt := now + AdminSessionTTL.Milliseconds()

	_, _ = h.db.Exec("INSERT INTO admin_sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)", tokenHash, now, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     AdminSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(AdminSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return token
}

// Clear admin session cookie
func (h *SRouterHandler) clearAdminSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(AdminSessionCookie); err == nil && cookie != nil {
		tokenHash := sha256Hex(cookie.Value)
		_, _ = h.db.Exec("DELETE FROM admin_sessions WHERE token_hash = ?", tokenHash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AdminSessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GET /v1/admin/status
func (h *SRouterHandler) HandleAdminStatus(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DEBUG] HandleAdminStatus called from %s method %s", r.RemoteAddr, r.Method)
	hasAdmin := h.hasAdminAccount()
	auth := h.verifyAdminSession(r)

	if !hasAdmin {
		envPass := os.Getenv("SROUTER_ADMIN_PASSWORD")
		if envPass == "" {
			envPass = os.Getenv("LAM_ADMIN_PASSWORD")
		}
		if envPass != "" {
			now := time.Now().UnixMilli()
			_, _ = h.db.Exec("INSERT OR REPLACE INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", HashPassword(envPass), now, now)
			hasAdmin = true
		}
	}

	res := map[string]any{
		"setupRequired": !hasAdmin,
		"authenticated": auth,
	}
	handlerutil.WriteJSON(w, http.StatusOK, res)
}

// POST /v1/admin/setup
func (h *SRouterHandler) HandleAdminSetup(w http.ResponseWriter, r *http.Request) {
	if h.hasAdminAccount() {
		handlerutil.WriteJSONError(w, http.StatusConflict, "Admin setup has already been completed")
		return
	}

	var body struct {
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if len(body.Password) < 1 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Password is required")
		return
	}
	if body.Confirmation != "" && body.Password != body.Confirmation {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Password confirmation does not match")
		return
	}

	now := time.Now().UnixMilli()
	passHash := HashPassword(body.Password)

	_, err := h.db.Exec("INSERT INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", passHash, now, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to save admin credentials")
		return
	}

	h.createAdminSession(w)
	handlerutil.WriteJSON(w, http.StatusCreated, map[string]any{"authenticated": true})
}

// POST /v1/admin/login
func (h *SRouterHandler) HandleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	storedHash, err := h.getAdminPasswordHash()
	if err != nil || storedHash == "" {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid admin password")
		return
	}

	if HashPassword(body.Password) != storedHash {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid admin password")
		return
	}

	h.createAdminSession(w)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// POST /v1/admin/change-password
func (h *SRouterHandler) HandleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	if !h.verifyAdminSession(r) {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Admin authentication is required")
		return
	}

	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		Confirmation    string `json:"confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	storedHash, err := h.getAdminPasswordHash()
	if err != nil || storedHash == "" || HashPassword(body.CurrentPassword) != storedHash {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Current admin password is incorrect")
		return
	}

	if len(body.NewPassword) < 1 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "New password is required")
		return
	}
	if body.NewPassword != body.Confirmation {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "New password confirmation does not match")
		return
	}

	now := time.Now().UnixMilli()
	newHash := HashPassword(body.NewPassword)
	_, err = h.db.Exec("UPDATE admin_account SET password_hash = ?, updated_at = ? WHERE id = 1", newHash, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to update admin password")
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"message": "Admin password updated successfully"})
}

// POST /v1/admin/logout
func (h *SRouterHandler) HandleAdminLogout(w http.ResponseWriter, r *http.Request) {
	h.clearAdminSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}
