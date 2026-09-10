package srouterapi

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os/exec"
	"sync"

	"github.com/go-chi/chi/v5"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/shared"
)

// InitSchema ensures the SRouter tables exist in the SQLite database.
func InitSchema(dbConn *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS admin_account (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    protocol TEXT NOT NULL,
    base_url TEXT,
    api_key TEXT,
    access_token TEXT,
    refresh_token TEXT,
    account_id TEXT,
    organization_id TEXT,
    token_expires_at INTEGER,
    last_refreshed_at INTEGER,
    custom_headers TEXT,
    provider_specific_data TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    key TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    rate_limit INTEGER DEFAULT 0,
    quota_limit INTEGER DEFAULT 0,
    usage_tokens INTEGER DEFAULT 0,
    credit_limit REAL DEFAULT 0,
    usage_cost REAL DEFAULT 0,
    allowed_models TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS request_logs (
    id TEXT PRIMARY KEY,
    api_key_id TEXT,
    provider_id TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost REAL NOT NULL DEFAULT 0,
    fallback_occurred INTEGER NOT NULL DEFAULT 0,
    fallback_path TEXT,
    fallback_reason TEXT,
    resolved_model TEXT
);

CREATE TABLE IF NOT EXISTS fallback_rules (
    id TEXT PRIMARY KEY,
    source_model TEXT NOT NULL,
    target_model TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 1,
    trigger_on_status TEXT,
    max_retries INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_sessions (
    state TEXT PRIMARY KEY,
    code_verifier TEXT NOT NULL,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    provider TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS custom_models (
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (provider_id, model_id)
);

CREATE TABLE IF NOT EXISTS provider_models (
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    display_name TEXT,
    context_length INTEGER DEFAULT 0,
    is_custom INTEGER DEFAULT 0,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (provider_id, model_id)
);

CREATE TABLE IF NOT EXISTS disabled_models (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT,
    reason TEXT,
    disabled_at INTEGER NOT NULL
);
`
	_, err := dbConn.Exec(schema)
	return err
}

// SHA256 helper
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Generate random hex
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// API Handler struct for SRouter compatibility
type SRouterHandler struct {
	repo *db.Repo
	db   *sql.DB
	ts   *shared.TokenSaverConfig

	// Cloudflare Tunnel in-memory state
	tunnelMu      sync.Mutex
	tunnelCmd     *exec.Cmd
	tunnelDomain  string
	tunnelRunning bool
	tunnelStarted int64
	tunnelError   string
}

func NewSRouterHandler(repo *db.Repo, ts *shared.TokenSaverConfig) *SRouterHandler {
	return &SRouterHandler{
		repo: repo,
		db:   repo.RawDB(),
		ts:   ts,
	}
}

// RegisterV1Routes mounts all SRouter-compatible /v1 endpoints on the router.
func RegisterV1Routes(r chi.Router, repo *db.Repo, ts *shared.TokenSaverConfig) {
	h := NewSRouterHandler(repo, ts)
	_ = InitSchema(repo.RawDB())

	// Admin Domain
	r.HandleFunc("/v1/admin/status", h.HandleAdminStatus)
	r.HandleFunc("/v1/admin/setup", h.HandleAdminSetup)
	r.HandleFunc("/v1/admin/login", h.HandleAdminLogin)
	r.HandleFunc("/v1/admin/change-password", h.HandleAdminChangePassword)
	r.HandleFunc("/v1/admin/logout", h.HandleAdminLogout)

	// Providers Domain
	r.HandleFunc("/v1/providers", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleProvidersList(w, req)
		} else if req.Method == http.MethodPost {
			h.HandleProviderAdd(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	r.HandleFunc("/v1/providers/catalog", h.HandleProvidersCatalog)
	r.HandleFunc("/v1/providers/verify", h.HandleProviderVerify)
	r.HandleFunc("/v1/providers/{providerId}", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleProviderGet(w, req)
		} else if req.Method == http.MethodDelete {
			h.HandleProviderDelete(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	r.HandleFunc("/v1/providers/{providerId}/models", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleProviderModelsList(w, req)
		} else if req.Method == http.MethodPost {
			h.HandleCustomModelAdd(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	r.HandleFunc("/v1/providers/{providerId}/sync", h.HandleProviderSyncModels)
	r.HandleFunc("/v1/providers/{providerId}/models/toggle", h.HandleProviderModelToggle)
	r.HandleFunc("/v1/providers/{providerId}/models/{modelId}/toggle", h.HandleProviderModelToggle)
	r.HandleFunc("/v1/providers/{providerId}/models/{modelId}", h.HandleCustomModelDelete)

	// Keys Domain
	r.HandleFunc("/v1/keys", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleKeysList(w, req)
		} else if req.Method == http.MethodPost {
			h.HandleKeyCreate(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	r.HandleFunc("/v1/keys/{id}/credit", h.HandleKeyAddCredit)
	r.HandleFunc("/v1/keys/{id}", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			h.HandleKeyDelete(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Settings Domain
	r.HandleFunc("/v1/settings", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleSettingsGet(w, req)
		} else {
			h.HandleSettingsUpdate(w, req)
		}
	})
	r.HandleFunc("/v1/settings/token-saver", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleTokenSaverGet(w, req)
		} else {
			h.HandleTokenSaverUpdate(w, req)
		}
	})
	r.HandleFunc("/v1/settings/token-saver/test", h.HandleTokenSaverTest)
	r.HandleFunc("/v1/settings/cache", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleCacheGet(w, req)
		} else if req.Method == http.MethodDelete {
			h.HandleCacheClear(w, req)
		} else {
			h.HandleCacheUpdate(w, req)
		}
	})
	r.HandleFunc("/v1/settings/fallbacks", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleFallbacksGet(w, req)
		} else {
			h.HandleFallbackCreate(w, req)
		}
	})
	r.HandleFunc("/v1/settings/fallbacks/{id}", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			h.HandleFallbackDelete(w, req)
		} else {
			h.HandleFallbackUpdate(w, req)
		}
	})

	// Full Database Backup Export & Import (9Router / SRouter / LAM-Router cross-compatible)
	r.HandleFunc("/v1/settings/backup/export", h.HandleBackupExport)
	r.HandleFunc("/v1/settings/backup/import", h.HandleBackupImport)

	// Logs & Quota Domain
	r.HandleFunc("/v1/logs", h.HandleLogsList)
	r.HandleFunc("/v1/logs/stats", h.HandleLogsStats)
	r.HandleFunc("/v1/quota", h.HandleQuotaGet)

	// Tunnel Domain
	r.HandleFunc("/v1/tunnel/status", h.HandleTunnelStatus)
	r.HandleFunc("/v1/tunnel/events", h.HandleTunnelEvents)
	r.HandleFunc("/v1/tunnel/start", h.HandleTunnelStart)
	r.HandleFunc("/v1/tunnel/stop", h.HandleTunnelStop)
	r.HandleFunc("/v1/tunnel/config", h.HandleTunnelConfig)
	r.HandleFunc("/v1/tunnel/install", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			h.HandleTunnelInstallStatus(w, req)
		} else {
			h.HandleTunnelInstall(w, req)
		}
	})

	// Auth OAuth Domain
	r.HandleFunc("/v1/auth/{provider}/login", h.HandleOAuthLogin)
	r.HandleFunc("/v1/auth/{provider}/poll", h.HandleOAuthPoll)
	r.HandleFunc("/v1/auth/{provider}/token", h.HandleOAuthToken)
	r.HandleFunc("/v1/auth/{provider}/callback", h.HandleOAuthCallback)
	r.HandleFunc("/v1/auth/framer/bridge.user.js", h.HandleFramerBridgeScript)
	r.HandleFunc("/v1/auth/framer/bridge", h.HandleFramerBridgeIngest)
	r.HandleFunc("/v1/auth/framer/session-sync", h.HandleFramerSessionSync)
}
