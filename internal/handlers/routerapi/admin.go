package routerapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"lamrouter/internal/handlerutil"
)

const (
	AdminSessionCookie = "lamrouter_admin_session"
	AdminSessionTTL    = 7 * 24 * time.Hour
)

// loginAttemptTracker tracks failed login attempts for brute-force rate limiting.
type loginAttemptTracker struct {
	mu       sync.Mutex
	attempts map[string][]int64
}

var loginLimiter = &loginAttemptTracker{
	attempts: make(map[string][]int64),
}

func (t *loginAttemptTracker) isRateLimited(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	cutoff := now - 60*1000 // 1 minute window

	timestamps := t.attempts[ip]
	valid := make([]int64, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts > cutoff {
			valid = append(valid, ts)
		}
	}
	t.attempts[ip] = valid

	return len(valid) >= 5 // max 5 failed attempts per minute
}

func (t *loginAttemptTracker) recordFailedAttempt(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	t.attempts[ip] = append(t.attempts[ip], now)
}

func (t *loginAttemptTracker) recordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.attempts, ip)
}

// HashPassword hashes a password using bcrypt with standard cost.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// verifyPassword verifies password against stored hash, supporting both bcrypt and legacy SHA-256.
// Returns (matched, needsUpgrade).
func verifyPassword(password, storedHash string) (bool, bool) {
	if strings.HasPrefix(storedHash, "$2a$") || strings.HasPrefix(storedHash, "$2b$") {
		err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
		return err == nil, false
	}

	// Legacy SHA-256 fallback with constant-time comparison
	legacySalt := "lam-router-salt"
	h := sha256.Sum256([]byte(legacySalt + ":" + password))
	legacyHash := hex.EncodeToString(h[:])
	if subtle.ConstantTimeCompare([]byte(legacyHash), []byte(storedHash)) == 1 {
		return true, true // valid, but needs upgrade to bcrypt
	}

	return false, false
}

// Check if admin account is configured
func (h *RouterAPIHandler) hasAdminAccount() bool {
	var count int
	err := h.db.QueryRow("SELECT COUNT(*) FROM admin_account WHERE id = 1").Scan(&count)
	return err == nil && count > 0
}

// Get admin password hash
func (h *RouterAPIHandler) getAdminPasswordHash() (string, error) {
	var hash string
	err := h.db.QueryRow("SELECT password_hash FROM admin_account WHERE id = 1").Scan(&hash)
	return hash, err
}

// Verify session from cookie or header
func (h *RouterAPIHandler) verifyAdminSession(r *http.Request) bool {
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

	// Check via Repo if available to support system secret tokens
	if h.repo != nil && h.repo.VerifyAdminSession(token) {
		return true
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
func (h *RouterAPIHandler) createAdminSession(w http.ResponseWriter, r *http.Request) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	tokenHash := sha256Hex(token)

	now := time.Now().UnixMilli()
	expiresAt := now + AdminSessionTTL.Milliseconds()

	_, _ = h.db.Exec("INSERT INTO admin_sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)", tokenHash, now, expiresAt)

	isSecure := false
	if r != nil {
		isSecure = r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AdminSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(AdminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})

	return token
}

// Clear admin session cookie
func (h *RouterAPIHandler) clearAdminSession(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleAdminStatus(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DEBUG] HandleAdminStatus called from %s method %s", r.RemoteAddr, r.Method)
	hasAdmin := h.hasAdminAccount()
	auth := h.verifyAdminSession(r)

	if !hasAdmin {
		envPass := os.Getenv("LAM_ADMIN_PASSWORD")
		if envPass == "" {
			envPass = os.Getenv("LAMROUTER_ADMIN_PASSWORD")
		}
		if envPass != "" {
			now := time.Now().UnixMilli()
			passHash, err := HashPassword(envPass)
			if err == nil {
				_, _ = h.db.Exec("INSERT OR REPLACE INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", passHash, now, now)
				hasAdmin = true
			}
		}
	}

	res := map[string]any{
		"setupRequired": !hasAdmin,
		"authenticated": auth,
	}
	handlerutil.WriteJSON(w, http.StatusOK, res)
}

// POST /v1/admin/setup
func (h *RouterAPIHandler) HandleAdminSetup(w http.ResponseWriter, r *http.Request) {
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
	passHash, err := HashPassword(body.Password)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	_, err = h.db.Exec("INSERT INTO admin_account (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)", passHash, now, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to save admin credentials")
		return
	}

	h.createAdminSession(w, r)
	handlerutil.WriteJSON(w, http.StatusCreated, map[string]any{"authenticated": true})
}

// POST /v1/admin/login
func (h *RouterAPIHandler) HandleAdminLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := handlerutil.GetClientIP(r)
	if loginLimiter.isRateLimited(clientIP) {
		handlerutil.WriteJSONError(w, http.StatusTooManyRequests, "Too many failed login attempts. Please wait 1 minute.")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	storedHash, err := h.getAdminPasswordHash()
	if err != nil || storedHash == "" {
		loginLimiter.recordFailedAttempt(clientIP)
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid admin password")
		return
	}

	valid, needsUpgrade := verifyPassword(body.Password, storedHash)
	if !valid {
		loginLimiter.recordFailedAttempt(clientIP)
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Invalid admin password")
		return
	}

	// Login succeeded: reset rate limiter
	loginLimiter.recordSuccess(clientIP)

	// Transparently upgrade legacy SHA-256 hash to bcrypt in database
	if needsUpgrade {
		if newBcryptHash, bErr := HashPassword(body.Password); bErr == nil {
			now := time.Now().UnixMilli()
			_, _ = h.db.Exec("UPDATE admin_account SET password_hash = ?, updated_at = ? WHERE id = 1", newBcryptHash, now)
		}
	}

	h.createAdminSession(w, r)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// minAdminPasswordLength is the shortest admin password the API accepts. The
// admin password guards every credential in the vault, so a single character is
// not a password; the UI hint must match this floor.
const minAdminPasswordLength = 8

// POST /v1/admin/change-password
func (h *RouterAPIHandler) HandleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || storedHash == "" {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Current admin password is incorrect")
		return
	}

	valid, _ := verifyPassword(body.CurrentPassword, storedHash)
	if !valid {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Current admin password is incorrect")
		return
	}

	if len(body.NewPassword) < 1 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "New password is required")
		return
	}
	if len([]rune(body.NewPassword)) < minAdminPasswordLength {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("New password must be at least %d characters", minAdminPasswordLength))
		return
	}
	if body.NewPassword != body.Confirmation {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "New password confirmation does not match")
		return
	}

	now := time.Now().UnixMilli()
	newHash, err := HashPassword(body.NewPassword)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to hash new password")
		return
	}

	_, err = h.db.Exec("UPDATE admin_account SET password_hash = ?, updated_at = ? WHERE id = 1", newHash, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to update admin password")
		return
	}

	// Invalidate ALL previous admin sessions to protect compromised sessions
	_, _ = h.db.Exec("DELETE FROM admin_sessions")

	// Issue fresh session for current client
	h.createAdminSession(w, r)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"message": "Admin password updated successfully"})
}

// POST /v1/admin/logout
func (h *RouterAPIHandler) HandleAdminLogout(w http.ResponseWriter, r *http.Request) {
	h.clearAdminSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}
