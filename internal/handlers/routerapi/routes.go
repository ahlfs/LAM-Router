package routerapi

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os/exec"
	"sync"

	"github.com/go-chi/chi/v5"
	"lamrouter/internal/db"
	"lamrouter/internal/handlers/shared"
	"lamrouter/internal/middleware"
	"lamrouter/internal/models"
	"lamrouter/internal/providers"
)

// InitSchema ensures the LAM-Router tables exist in the SQLite database.
func InitSchema(dbConn *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS providerConnections (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    authType TEXT,
    name TEXT,
    email TEXT,
    priority INTEGER DEFAULT 999999,
    isActive INTEGER DEFAULT 1,
    data TEXT,
    createdAt TEXT,
    updatedAt TEXT
);

CREATE TABLE IF NOT EXISTS apiKeys (
    id TEXT PRIMARY KEY,
    name TEXT,
    key TEXT UNIQUE NOT NULL,
    keyHash TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    rateLimit INTEGER DEFAULT 0,
    quotaLimit INTEGER DEFAULT 0,
    usageTokens INTEGER DEFAULT 0,
    creditLimit REAL DEFAULT 0,
    usageCost REAL DEFAULT 0,
    allowedModels TEXT,
    createdAt TEXT,
    updatedAt TEXT
);

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
    priority INTEGER DEFAULT 999999,
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
    api_key_name TEXT,
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
    compression_tokens INTEGER NOT NULL DEFAULT 0,
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
    context_source TEXT DEFAULT '',
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

CREATE TABLE IF NOT EXISTS usageHistory (
    timestamp TEXT,
    provider TEXT,
    model TEXT,
    connectionId TEXT,
    apiKey TEXT,
    endpoint TEXT,
    promptTokens INTEGER,
    completionTokens INTEGER,
    cost REAL,
    status TEXT,
    tokens TEXT,
    meta TEXT
);

CREATE TABLE IF NOT EXISTS usageDaily (
    dateKey TEXT PRIMARY KEY,
    data TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS requestDetails (
    id TEXT PRIMARY KEY,
    timestamp TEXT,
    provider TEXT,
    model TEXT,
    connectionId TEXT,
    status TEXT,
    data TEXT
);

CREATE TABLE IF NOT EXISTS proxyPools (
    id TEXT PRIMARY KEY,
    isActive INTEGER DEFAULT 1,
    testStatus TEXT,
    data TEXT,
    createdAt TEXT,
    updatedAt TEXT
);

CREATE TABLE IF NOT EXISTS model_health (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    status TEXT NOT NULL,
    latency_ms INTEGER DEFAULT 0,
    status_code INTEGER DEFAULT 0,
    last_error TEXT,
    last_checked_at INTEGER NOT NULL
);
`
	_, err := dbConn.Exec(schema)
	if err == nil {
		// Migration for existing tables
		_, _ = dbConn.Exec(`ALTER TABLE providers ADD COLUMN priority INTEGER DEFAULT 999999`)
		_, _ = dbConn.Exec(`ALTER TABLE providerConnections ADD COLUMN priority INTEGER DEFAULT 999999`)
		_, _ = dbConn.Exec(`ALTER TABLE request_logs ADD COLUMN compression_tokens INTEGER DEFAULT 0`)
		_, _ = dbConn.Exec(`ALTER TABLE request_logs ADD COLUMN api_key_name TEXT`)
		_, _ = dbConn.Exec(`ALTER TABLE provider_models ADD COLUMN context_source TEXT DEFAULT ''`)
		// prompt inject: kolom baru di combos dan fallback_rules
		_, _ = dbConn.Exec(`ALTER TABLE combos ADD COLUMN system_prompt TEXT DEFAULT ''`)
		_, _ = dbConn.Exec(`ALTER TABLE combos ADD COLUMN inject_mode TEXT DEFAULT 'prepend'`)
		_, _ = dbConn.Exec(`ALTER TABLE combos ADD COLUMN prompt_injection_enabled INTEGER`)
		_, _ = dbConn.Exec(`ALTER TABLE combos ADD COLUMN display_identity TEXT`)
		_, _ = dbConn.Exec(`ALTER TABLE fallback_rules ADD COLUMN system_prompt TEXT DEFAULT ''`)
		_, _ = dbConn.Exec(`ALTER TABLE fallback_rules ADD COLUMN inject_mode TEXT DEFAULT 'prepend'`)
		_, _ = dbConn.Exec(`ALTER TABLE fallback_rules ADD COLUMN prompt_injection_enabled INTEGER`)
		_, _ = dbConn.Exec(`ALTER TABLE fallback_rules ADD COLUMN display_identity TEXT`)
		_, _ = dbConn.Exec(`UPDATE provider_models SET context_source = 'heuristic' WHERE COALESCE(context_source, '') = ''`)
		migrateRequestLogAPIKeys(dbConn)
		repairModelContextWindows(dbConn)
		_, _ = dbConn.Exec(`ALTER TABLE apiKeys ADD COLUMN isActive INTEGER DEFAULT 1`)
		_, _ = dbConn.Exec(`ALTER TABLE apiKeys ADD COLUMN enabled INTEGER DEFAULT 1`)

		h := &RouterAPIHandler{db: dbConn}
		_ = h.InitHealthSchema()
	}
	return err
}

// migrateRequestLogAPIKeys upgrades audit rows written before client key
// identity tracking existed. Those rows hold either the raw client key or, for
// requests with no client key, a masked upstream credential. Both are rewritten
// to the stable api_keys identity so the audit trail says which key was used
// without keeping a usable secret in the table. Idempotent; safe to run on every
// start and on databases that have no api_keys table yet.
func migrateRequestLogAPIKeys(dbConn *sql.DB) {
	// Rows already carrying an api_keys.id gain the display name of that key.
	_, _ = dbConn.Exec(`
		UPDATE request_logs
		SET api_key_name = (SELECT name FROM api_keys WHERE api_keys.id = request_logs.api_key_id)
		WHERE (api_key_name IS NULL OR api_key_name = '')
		  AND api_key_id IN (SELECT id FROM api_keys)`)
	// Rows carrying the raw client key are re-pointed at that key's identity.
	_, _ = dbConn.Exec(`
		UPDATE request_logs
		SET api_key_id = (SELECT id FROM api_keys WHERE api_keys.key = request_logs.api_key_id),
		    api_key_name = (SELECT name FROM api_keys WHERE api_keys.key = request_logs.api_key_id)
		WHERE api_key_id IN (SELECT key FROM api_keys)`)
	// Dashboard sessions used to be recorded under the bare marker "admin".
	_, _ = dbConn.Exec(`
		UPDATE request_logs
		SET api_key_id = ?, api_key_name = ?
		WHERE api_key_id = 'admin'`, models.AdminSessionKeyID, models.AdminSessionKeyName)
	// A raw router key whose row was deleted must not survive readable.
	_, _ = dbConn.Exec(`
		UPDATE request_logs
		SET api_key_id = substr(api_key_id, 1, 9) || '…' || substr(api_key_id, -4)
		WHERE api_key_id LIKE 'lam-live-%'
		  AND api_key_id NOT IN (SELECT key FROM api_keys)`)
	// Non-key markers keep their id but still get the label the dashboard shows.
	_, _ = dbConn.Exec(`UPDATE request_logs SET api_key_name = ? WHERE api_key_id = ?`,
		models.NoClientKeyName, models.NoClientKeyID)
	_, _ = dbConn.Exec(`UPDATE request_logs SET api_key_name = ? WHERE api_key_id LIKE 'unmatched:%'`,
		models.UnknownClientKeyName)
}

// repairModelContextWindows fixes provider_models rows written by older syncs that
// stamped every discovered model with a flat 128k context window. A model whose
// family is known to be larger (gemini 1M, claude/o-series 200k, deepseek-v4 1M)
// is lifted to that size so /v1/models and the dashboard stop under-reporting.
// Manual overrides are never touched and rows already at or above the family size
// are left alone, making this idempotent.
func repairModelContextWindows(dbConn *sql.DB) {
	rows, err := dbConn.Query(`SELECT provider_id, model_id, COALESCE(context_length, 0)
		FROM provider_models
		WHERE COALESCE(context_length, 0) <= 131072 AND COALESCE(context_source, '') <> 'manual'`)
	if err != nil {
		return
	}
	type fix struct {
		provider, model string
		ctx             int
	}
	var fixes []fix
	for rows.Next() {
		var providerID, modelID string
		var ctxLen int
		if err := rows.Scan(&providerID, &modelID, &ctxLen); err != nil {
			continue
		}
		want, _ := providers.GetModelTokenLimits(modelID)
		// Only upgrade when the family table knows better than the stored default.
		if want > 131072 && want > ctxLen {
			fixes = append(fixes, fix{providerID, modelID, want})
		}
	}
	rows.Close()
	for _, f := range fixes {
		_, _ = dbConn.Exec(`UPDATE provider_models SET context_length = ?, context_source = 'heuristic'
			WHERE provider_id = ? AND model_id = ? AND COALESCE(context_source, '') <> 'manual'`,
			f.ctx, f.provider, f.model)
	}
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

// API Handler struct for LAM-Router API compatibility
type RouterAPIHandler struct {
	repo *db.Repo
	db   *sql.DB
	ts   *shared.TokenSaverConfig

	ChatHandler http.HandlerFunc

	// Cloudflare Tunnel in-memory state
	tunnelMu      sync.Mutex
	tunnelCmd     *exec.Cmd
	tunnelDomain  string
	tunnelRunning bool
	tunnelStarted int64
	tunnelError   string
}

func NewRouterAPIHandler(repo *db.Repo, ts *shared.TokenSaverConfig) *RouterAPIHandler {
	return &RouterAPIHandler{
		repo: repo,
		db:   repo.RawDB(),
		ts:   ts,
	}
}

func (h *RouterAPIHandler) SetChatHandler(fn http.HandlerFunc) {
	h.ChatHandler = fn
}

// RegisterV1Routes mounts all LAM-Router /v1 endpoints on the router.
func RegisterV1Routes(r chi.Router, repo *db.Repo, ts *shared.TokenSaverConfig) *RouterAPIHandler {
	h := NewRouterAPIHandler(repo, ts)
	_ = InitSchema(repo.RawDB())

	// Public Admin Status, Setup, and Login Endpoints (available at /v1/admin/* and aliased to /admin/*)
	r.HandleFunc("/v1/admin/status", h.HandleAdminStatus)
	r.HandleFunc("/admin/status", h.HandleAdminStatus)
	r.HandleFunc("/v1/admin/setup", h.HandleAdminSetup)
	r.HandleFunc("/admin/setup", h.HandleAdminSetup)
	r.HandleFunc("/v1/admin/login", h.HandleAdminLogin)
	r.HandleFunc("/admin/login", h.HandleAdminLogin)

	// Public OAuth Callbacks
	r.HandleFunc("/v1/auth/{provider}/open", h.HandleOAuthOpen)
	r.HandleFunc("/v1/auth/{provider}/callback", h.HandleOAuthCallback)
	r.HandleFunc("/v1/auth/{provider}/poll", h.HandleOAuthPoll)

	// Protected Admin Domain Routes (requires valid admin session cookie or admin token)
	r.Group(func(admin chi.Router) {
		admin.Use(middleware.RequireAdminSession(repo))

		// Admin management
		admin.HandleFunc("/v1/admin/change-password", h.HandleAdminChangePassword)
		admin.HandleFunc("/admin/change-password", h.HandleAdminChangePassword)
		admin.HandleFunc("/v1/admin/logout", h.HandleAdminLogout)
		admin.HandleFunc("/admin/logout", h.HandleAdminLogout)

		// Providers Domain
		admin.HandleFunc("/v1/providers", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleProvidersList(w, req)
			} else if req.Method == http.MethodPost {
				h.HandleProviderAdd(w, req)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})
		admin.HandleFunc("/v1/providers/catalog", h.HandleProvidersCatalog)
		admin.HandleFunc("/v1/providers/verify", h.HandleProviderVerify)
		admin.HandleFunc("/v1/providers/{providerId}", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleProviderGet(w, req)
			} else if req.Method == http.MethodDelete {
				h.HandleProviderDelete(w, req)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})
		admin.HandleFunc("/v1/providers/{providerId}/models", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleProviderModelsList(w, req)
			} else if req.Method == http.MethodPost {
				h.HandleCustomModelAdd(w, req)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})
		admin.HandleFunc("/v1/providers/{providerId}/sync", h.HandleProviderSyncModels)
		admin.HandleFunc("/v1/providers/{providerId}/models/toggle", h.HandleProviderModelToggle)
		admin.HandleFunc("/v1/providers/{providerId}/models/context", h.HandleProviderModelContext)
		admin.HandleFunc("/v1/prompt-inject", h.HandlePromptInjectGet)
		admin.HandleFunc("/v1/prompt-inject/set", h.HandlePromptInjectSet)
		admin.HandleFunc("/v1/prompt-inject/preview", h.HandlePromptInjectPreview)
		admin.HandleFunc("/v1/providers/{providerId}/models/{modelId}/toggle", h.HandleProviderModelToggle)
		admin.HandleFunc("/v1/providers/{providerId}/models/{modelId}", h.HandleCustomModelDelete)
		admin.HandleFunc("/v1/providers/{providerId}/reorder", h.HandleProviderReorderConnections)

		// Keys Domain
		admin.HandleFunc("/v1/keys", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleKeysList(w, req)
			} else if req.Method == http.MethodPost {
				h.HandleKeyCreate(w, req)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})
		admin.HandleFunc("/v1/keys/{id}/credit", h.HandleKeyAddCredit)
		admin.HandleFunc("/v1/keys/{id}", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodDelete {
				h.HandleKeyDelete(w, req)
			} else if req.Method == http.MethodPut || req.Method == http.MethodPatch {
				h.HandleKeyUpdate(w, req)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})

		// Settings Domain
		admin.HandleFunc("/v1/settings", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleSettingsGet(w, req)
			} else {
				h.HandleSettingsUpdate(w, req)
			}
		})
		admin.HandleFunc("/v1/settings/preferences", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandlePreferencesGet(w, req)
			} else {
				h.HandlePreferencesUpdate(w, req)
			}
		})
		admin.HandleFunc("/v1/settings/token-saver", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleTokenSaverGet(w, req)
			} else {
				h.HandleTokenSaverUpdate(w, req)
			}
		})
		admin.HandleFunc("/v1/settings/token-saver/test", h.HandleTokenSaverTest)
		admin.HandleFunc("/v1/settings/cache", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleCacheGet(w, req)
			} else if req.Method == http.MethodDelete {
				h.HandleCacheClear(w, req)
			} else {
				h.HandleCacheUpdate(w, req)
			}
		})
		admin.HandleFunc("/v1/settings/fallbacks", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleFallbacksGet(w, req)
			} else {
				h.HandleFallbackCreate(w, req)
			}
		})
		admin.HandleFunc("/v1/settings/fallbacks/{id}", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodDelete {
				h.HandleFallbackDelete(w, req)
			} else {
				h.HandleFallbackUpdate(w, req)
			}
		})

		// Full Database Backup Export & Import (LAM-Router database backup)
		admin.HandleFunc("/v1/settings/backup/export", h.HandleBackupExport)
		admin.HandleFunc("/v1/settings/backup/import", h.HandleBackupImport)

		// Logs & Quota Domain
		admin.HandleFunc("/v1/logs", h.HandleLogsList)
		admin.HandleFunc("/v1/logs/stats", h.HandleLogsStats)
		admin.HandleFunc("/v1/telemetry/active", h.HandleTelemetryActive)
		admin.HandleFunc("/v1/quota", h.HandleQuotaGet)

		// Health Domain
		admin.HandleFunc("/v1/health/models", h.HandleModelsHealthList)
		admin.HandleFunc("/v1/health/models/probe", h.HandleModelsHealthProbe)
		admin.HandleFunc("/v1/health/models/reset", h.HandleModelsHealthReset)
		admin.HandleFunc("/v1/health/models/disable-offline", h.HandleModelsHealthDisableOffline)
		admin.HandleFunc("/v1/health/models/toggle", h.HandleModelsHealthToggle)

		// Tunnel Domain
		admin.HandleFunc("/v1/tunnel/status", h.HandleTunnelStatus)
		admin.HandleFunc("/v1/tunnel/events", h.HandleTunnelEvents)
		admin.HandleFunc("/v1/tunnel/start", h.HandleTunnelStart)
		admin.HandleFunc("/v1/tunnel/stop", h.HandleTunnelStop)
		admin.HandleFunc("/v1/tunnel/config", h.HandleTunnelConfig)
		admin.HandleFunc("/v1/tunnel/install", func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet {
				h.HandleTunnelInstallStatus(w, req)
			} else {
				h.HandleTunnelInstall(w, req)
			}
		})

		// Auth OAuth Admin triggers
		admin.HandleFunc("/v1/auth/{provider}/login", h.HandleOAuthLogin)
		admin.HandleFunc("/v1/auth/{provider}/token", h.HandleOAuthToken)
	})

	return h
}
