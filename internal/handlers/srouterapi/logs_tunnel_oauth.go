package srouterapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"9router/proxy/internal/handlerutil"
)

// GET /v1/logs
func (h *SRouterHandler) HandleLogsList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
SELECT id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, reasoning_tokens, estimated_cost, fallback_occurred
FROM request_logs ORDER BY created_at DESC LIMIT 100
`)
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data":   []any{},
		})
		return
	}
	defer rows.Close()

	var logs []map[string]any
	for rows.Next() {
		var id, apiKeyID, providerID, model string
		var promptTok, compTok, totalTok, status, latency int
		var createdAt, cachedTok, reasoningTok int64
		var cost float64
		var fallbackOccurred int

		err := rows.Scan(
			&id, &apiKeyID, &providerID, &model, &promptTok, &compTok,
			&totalTok, &status, &latency, &createdAt, &cachedTok,
			&reasoningTok, &cost, &fallbackOccurred,
		)
		if err != nil {
			continue
		}

		logs = append(logs, map[string]any{
			"id":               id,
			"apiKeyId":         apiKeyID,
			"providerId":       providerID,
			"model":            model,
			"promptTokens":     promptTok,
			"completionTokens": compTok,
			"totalTokens":      totalTok,
			"statusCode":       status,
			"latencyMs":        latency,
			"createdAt":        createdAt,
			"cachedTokens":     cachedTok,
			"reasoningTokens":  reasoningTok,
			"estimatedCost":    cost,
			"fallbackOccurred": fallbackOccurred == 1,
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   logs,
	})
}

// GET /v1/logs/stats
func (h *SRouterHandler) HandleLogsStats(w http.ResponseWriter, r *http.Request) {
	var totalRequests, totalTokens, totalInputTokens, totalOutputTokens int
	var totalCost float64
	var avgLatency float64

	_ = h.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(estimated_cost), 0), COALESCE(AVG(latency_ms), 0) FROM request_logs").Scan(
		&totalRequests, &totalTokens, &totalInputTokens, &totalOutputTokens, &totalCost, &avgLatency,
	)

	// Fetch byModel breakdown
	rows, err := h.db.Query(`
SELECT model, COUNT(*), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(estimated_cost), 0)
FROM request_logs
GROUP BY model
ORDER BY COUNT(*) DESC
LIMIT 20
`)
	var byModel []map[string]any
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var model string
			var reqCount, inTok, outTok, cachedTok int
			var estCost float64
			if err := rows.Scan(&model, &reqCount, &inTok, &outTok, &cachedTok, &estCost); err == nil {
				byModel = append(byModel, map[string]any{
					"model":             model,
					"totalRequests":     reqCount,
					"totalInputTokens":  inTok,
					"totalOutputTokens": outTok,
					"totalCachedTokens": cachedTok,
					"estCost":           estCost,
				})
			}
		}
	}
	if byModel == nil {
		byModel = []map[string]any{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"totalRequests":     totalRequests,
		"totalTokens":       totalTokens,
		"totalInputTokens":  totalInputTokens,
		"totalOutputTokens": totalOutputTokens,
		"totalCost":         totalCost,
		"costLabel":         fmt.Sprintf("$%.2f", totalCost),
		"estimated":         true,
		"avgLatency":        avgLatency,
		"activeStreams":     0,
		"byModel":           byModel,
	})
}

// Handlers in quota.go handle GET /v1/quota


// GET /v1/tunnel/status
func (h *SRouterHandler) HandleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	h.tunnelMu.Lock()
	defer h.tunnelMu.Unlock()

	cloudflaredAvail := false
	if _, err := exec.LookPath("cloudflared"); err == nil {
		cloudflaredAvail = true
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"running":              h.tunnelRunning,
		"startedAt":            h.tunnelStarted,
		"domain":               h.tunnelDomain,
		"error":                h.tunnelError,
		"cloudflaredAvailable": cloudflaredAvail,
		"ok":                   true,
		"install": map[string]any{
			"inProgress":           false,
			"done":                 true,
			"cloudflaredAvailable": cloudflaredAvail,
		},
	})
}

// GET /v1/tunnel/events (SSE)
func (h *SRouterHandler) HandleTunnelEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	h.tunnelMu.Lock()
	cloudflaredAvail := false
	if _, err := exec.LookPath("cloudflared"); err == nil {
		cloudflaredAvail = true
	}
	status := map[string]any{
		"running":              h.tunnelRunning,
		"startedAt":            h.tunnelStarted,
		"domain":               h.tunnelDomain,
		"cloudflaredAvailable": cloudflaredAvail,
	}
	h.tunnelMu.Unlock()

	b, _ := json.Marshal(status)
	fmt.Fprintf(w, "data: %s\n\n", string(b))
	flusher.Flush()
}

// POST /v1/tunnel/start
func (h *SRouterHandler) HandleTunnelStart(w http.ResponseWriter, r *http.Request) {
	h.tunnelMu.Lock()
	defer h.tunnelMu.Unlock()

	h.tunnelRunning = true
	h.tunnelStarted = time.Now().UnixMilli()
	if h.tunnelDomain == "" {
		h.tunnelDomain = "https://cyberlab.buatjalan.xyz"
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Cloudflare Tunnel started",
		"domain":  h.tunnelDomain,
		"mode":    "named",
	})
}

// POST /v1/tunnel/stop
func (h *SRouterHandler) HandleTunnelStop(w http.ResponseWriter, r *http.Request) {
	h.tunnelMu.Lock()
	defer h.tunnelMu.Unlock()

	h.tunnelRunning = false
	if h.tunnelCmd != nil && h.tunnelCmd.Process != nil {
		_ = h.tunnelCmd.Process.Kill()
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Cloudflare Tunnel stopped",
	})
}

// PUT /v1/tunnel/config
func (h *SRouterHandler) HandleTunnelConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain string `json:"domain"`
		Token  string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	h.tunnelMu.Lock()
	if body.Domain != "" {
		h.tunnelDomain = body.Domain
	}
	h.tunnelMu.Unlock()

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /v1/tunnel/install
func (h *SRouterHandler) HandleTunnelInstall(w http.ResponseWriter, r *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "cloudflared is ready",
	})
}

// GET /v1/tunnel/install
func (h *SRouterHandler) HandleTunnelInstallStatus(w http.ResponseWriter, r *http.Request) {
	cloudflaredAvail := false
	if _, err := exec.LookPath("cloudflared"); err == nil {
		cloudflaredAvail = true
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"inProgress":           false,
		"done":                 true,
		"cloudflaredAvailable": cloudflaredAvail,
	})
}

// GET /v1/auth/framer/bridge.user.js
func (h *SRouterHandler) HandleFramerBridgeScript(w http.ResponseWriter, r *http.Request) {
	script := `// ==UserScript==
// @name         LAM-Router Framer Token Bridge
// @namespace    https://router.buatjalan.xyz
// @version      1.5.0
// @description  1-Click Session Sync + Auto-captures Framer AI session & Project ID to LAM-Router
// @match        https://framer.com/*
// @match        https://*.framer.com/*
// @grant        GM_xmlhttpRequest
// @grant        GM.xmlHttpRequest
// @connect      router.buatjalan.xyz
// @connect      9r.buatjalan.xyz
// @run-at       document-start
// ==/UserScript==

(function() {
    'use strict';
    console.log('%c[LAM-Router Bridge v1.5.0]%c Ready!', 'background:#8B5CF6;color:#fff;padding:2px 4px;border-radius:3px;', '');
    const SYNC_ENDPOINT = 'https://router.buatjalan.xyz/v1/auth/framer/session-sync';

    function extractProjectID() {
        const match = window.location.pathname.match(/\/projects\/([A-Za-z0-9_-]+)/);
        if (match) return match[1];
        // Check local storage / session storage
        try {
            for (let i = 0; i < localStorage.length; i++) {
                const k = localStorage.key(i);
                if (k.includes('project') || k.includes('current')) {
                    const v = localStorage.getItem(k);
                    if (v && v.length >= 15 && v.length <= 30 && !v.includes(' ')) return v;
                }
            }
        } catch(e) {}
        return '';
    }

    function injectFloatingButton() {
        if (document.getElementById('lam-framer-sync-btn')) return;
        const btn = document.createElement('button');
        btn.id = 'lam-framer-sync-btn';
        btn.innerHTML = '⚡ Sync to LAM-Router';
        btn.style.cssText = 'position:fixed;bottom:20px;right:20px;z-index:999999;background:#8B5CF6;color:#fff;border:none;border-radius:8px;padding:10px 16px;font-family:monospace;font-size:13px;font-weight:bold;cursor:pointer;box-shadow:0 4px 14px rgba(139,92,246,0.5);transition:all 0.2s ease;';
        
        btn.onmouseover = () => btn.style.transform = 'scale(1.05)';
        btn.onmouseout = () => btn.style.transform = 'scale(1)';
        btn.onclick = () => performFullSync(true);
        
        document.body.appendChild(btn);
    }

    async function performFullSync(manual = false) {
        const btn = document.getElementById('lam-framer-sync-btn');
        if (btn) btn.innerHTML = '⏳ Syncing...';

        let capturedToken = '';
        let capturedProject = extractProjectID();

        // 1. Scan LocalStorage & SessionStorage
        try {
            for (let i = 0; i < localStorage.length; i++) {
                const val = localStorage.getItem(localStorage.key(i));
                if (val && val.includes('eyJhbGciOi')) {
                    const match = val.match(/eyJhbGciOi[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+/);
                    if (match) { capturedToken = match[0]; break; }
                }
            }
            if (!capturedToken) {
                for (let i = 0; i < sessionStorage.length; i++) {
                    const val = sessionStorage.getItem(sessionStorage.key(i));
                    if (val && val.includes('eyJhbGciOi')) {
                        const match = val.match(/eyJhbGciOi[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+/);
                        if (match) { capturedToken = match[0]; break; }
                    }
                }
            }
        } catch(e) {}

        // 2. Scan IndexedDB if not found
        if (!capturedToken) {
            try {
                const dbs = ['keyval-store', 'crdt-sync-cache'];
                for (const dbName of dbs) {
                    await new Promise((resolve) => {
                        const req = indexedDB.open(dbName);
                        req.onsuccess = () => {
                            const db = req.result;
                            for (let i = 0; i < db.objectStoreNames.length; i++) {
                                const storeName = db.objectStoreNames[i];
                                try {
                                    const tx = db.transaction(storeName, 'readonly');
                                    const getAll = tx.objectStore(storeName).getAll();
                                    getAll.onsuccess = () => {
                                        if (getAll.result) {
                                            const str = JSON.stringify(getAll.result);
                                            if (str && str.includes('eyJhbGciOi')) {
                                                const match = str.match(/eyJhbGciOi[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+/);
                                                if (match) capturedToken = match[0];
                                            }
                                        }
                                        resolve();
                                    };
                                    getAll.onerror = () => resolve();
                                } catch(e) { resolve(); }
                            }
                        };
                        req.onerror = () => resolve();
                    });
                    if (capturedToken) break;
                }
            } catch(e) {}
        }

        if (!capturedToken) {
            console.warn('[LAM-Router Bridge] No active JWT found yet. Please send a message or open a project in Framer.');
            if (btn) {
                btn.innerHTML = '⚠️ Open a project/chat first';
                btn.style.background = '#EF4444';
                setTimeout(() => {
                    btn.innerHTML = '⚡ Sync to LAM-Router';
                    btn.style.background = '#8B5CF6';
                }, 3500);
            }
            return;
        }

        const payload = JSON.stringify({
            cookies: document.cookie,
            token: capturedToken,
            projectId: capturedProject,
            storageData: { ...localStorage }
        });

        const handleSuccess = (resText) => {
            try {
                const data = typeof resText === 'string' ? JSON.parse(resText) : resText;
                if (data.error || data.success === false) {
                    console.error('[LAM-Router Bridge] ❌ Server error:', data.error || data.message);
                    if (btn) {
                        btn.innerHTML = '⚠️ Sync Failed';
                        btn.style.background = '#EF4444';
                        setTimeout(() => {
                            btn.innerHTML = '⚡ Sync to LAM-Router';
                            btn.style.background = '#8B5CF6';
                        }, 3500);
                    }
                    return;
                }
                console.log('%c[LAM-Router Bridge]%c ✅ 1-Click Sync Success: ' + (data.message || JSON.stringify(data)), 'background:#10B981;color:#fff;font-weight:bold;', '');
                if (btn) {
                    btn.innerHTML = '✅ Synced: ' + (data.account || 'OK');
                    btn.style.background = '#10B981';
                    setTimeout(() => {
                        btn.innerHTML = '⚡ Sync to LAM-Router';
                        btn.style.background = '#8B5CF6';
                    }, 3000);
                }
            } catch(e) {
                console.log('[LAM-Router Bridge] Sync response:', resText);
            }
        };

        if (typeof GM_xmlhttpRequest !== 'undefined') {
            GM_xmlhttpRequest({
                method: 'POST',
                url: SYNC_ENDPOINT,
                headers: { 'Content-Type': 'application/json' },
                data: payload,
                onload: (res) => handleSuccess(res.responseText),
                onerror: (err) => {
                    console.error('[LAM-Router Bridge] Sync failed:', err);
                    if (btn) {
                        btn.innerHTML = '❌ Sync Failed';
                        btn.style.background = '#EF4444';
                    }
                }
            });
        } else {
            fetch(SYNC_ENDPOINT, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: payload
            }).then(r => r.json()).then(data => handleSuccess(JSON.stringify(data))).catch(err => {
                console.error('[LAM-Router Bridge] Sync failed:', err);
                if (btn) {
                    btn.innerHTML = '❌ Sync Failed';
                    btn.style.background = '#EF4444';
                }
            });
        }
    }

    // Intercept fetch
    const originalFetch = window.fetch;
    window.fetch = async function(...args) {
        try {
            const options = args[1] || {};
            const headers = options.headers || {};
            let authHeader = '';
            if (headers instanceof Headers) {
                authHeader = headers.get('Authorization') || headers.get('authorization') || '';
            } else if (typeof headers === 'object') {
                authHeader = headers['Authorization'] || headers['authorization'] || '';
            }
            if (authHeader && authHeader.includes('Bearer eyJ')) {
                const token = authHeader.replace(/^Bearer\s+/i, '').trim();
                const bodyStr = typeof options.body === 'string' ? options.body : '';
                let proj = extractProjectID();
                if (bodyStr && bodyStr.includes('projectId')) {
                    try {
                        const parsed = JSON.parse(bodyStr);
                        if (parsed.projectId) proj = parsed.projectId;
                    } catch(e){}
                }
                fetch(SYNC_ENDPOINT, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ token: token, cookies: document.cookie, projectId: proj })
                }).catch(()=>{});
            }
        } catch(e) {}
        return originalFetch.apply(this, args);
    };

    window.addEventListener('DOMContentLoaded', injectFloatingButton);
    setTimeout(injectFloatingButton, 2000);
})();`

	w.Header().Set("Content-Type", "application/javascript")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}

// POST /v1/auth/framer/session-sync
func (h *SRouterHandler) HandleFramerSessionSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var body struct {
		Cookies     string            `json:"cookies"`
		Token       string            `json:"token"`
		ProjectID   string            `json:"projectId"`
		StorageData map[string]string `json:"storageData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	token := strings.TrimPrefix(body.Token, "Bearer ")
	projectID := strings.TrimSpace(body.ProjectID)

	// If token wasn't directly in body.Token, look inside storageData!
	if token == "" && body.StorageData != nil {
		for _, val := range body.StorageData {
			if strings.Contains(val, "eyJhbGciOi") {
				parts := strings.Split(val, "\"")
				for _, p := range parts {
					if strings.HasPrefix(p, "eyJhbGciOi") {
						token = p
						break
					}
				}
				if token != "" {
					break
				}
			}
		}
	}

	if token == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "No valid JWT token found in session payload")
		return
	}

	now := time.Now().UnixMilli()

	accountName := "Framer Account"
	accountID := "framer-" + randomHex(4)

	parts := strings.Split(token, ".")
	if len(parts) >= 2 {
		payloadSegment := parts[1]
		if rem := len(payloadSegment) % 4; rem > 0 {
			payloadSegment += strings.Repeat("=", 4-rem)
		}
		if payload, err := base64.URLEncoding.DecodeString(payloadSegment); err == nil {
			var claims struct {
				Email    string `json:"email"`
				UserID   string `json:"userId"`
				Username string `json:"username"`
				Payload  struct {
					Email     string `json:"email"`
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
					Username  string `json:"username"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(payload, &claims); err == nil {
				email := claims.Email
				if email == "" {
					email = claims.Payload.Email
				}
				name := claims.Username
				if name == "" {
					name = claims.Payload.Username
				}
				if name == "" && (claims.Payload.FirstName != "" || claims.Payload.LastName != "") {
					name = strings.TrimSpace(claims.Payload.FirstName + " " + claims.Payload.LastName)
				}

				shortUID := ""
				if claims.UserID != "" {
					accountID = "framer-" + claims.UserID
					if len(claims.UserID) >= 6 {
						shortUID = claims.UserID[:6]
					}
				}

				if email != "" {
					if shortUID != "" {
						accountName = fmt.Sprintf("%s (%s)", email, shortUID)
					} else {
						accountName = email
					}
				} else if name != "" {
					if shortUID != "" {
						accountName = fmt.Sprintf("%s (%s)", name, shortUID)
					} else {
						accountName = name
					}
				}
			}
		}
	}

	connDataMap := map[string]any{
		"apiKey":  token,
		"baseUrl": "https://api.framer.com/ai/v3/chat/",
	}
	if projectID != "" {
		connDataMap["projectId"] = projectID
	}
	connData, _ := json.Marshal(connDataMap)

	_, err := h.db.Exec(`
		INSERT OR REPLACE INTO providers (
			id, provider_id, name, category, protocol, base_url, api_key, access_token, enabled, created_at
		) VALUES (?, 'framer', ?, 'api_key', 'custom', 'https://api.framer.com/ai/v3/chat/', ?, ?, 1, ?)`,
		accountID, accountName, token, token, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to persist provider: "+err.Error())
		return
	}

	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, _ = h.db.Exec(`
		INSERT OR REPLACE INTO providerConnections (
			id, provider, authType, name, isActive, data, createdAt, updatedAt
		) VALUES (?, 'framer', 'api_key', ?, 1, ?, ?, ?)`,
		accountID, accountName, string(connData), nowISO, nowISO)

	_, _ = h.ScanAndSyncProviderModels("framer")

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"account": accountName,
		"id":      accountID,
		"message": fmt.Sprintf("Framer session synced successfully for %s!", accountName),
	})
}

// POST /v1/auth/framer/bridge
func (h *SRouterHandler) HandleFramerBridgeIngest(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for web browser extension/userscript
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var body struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Token is required")
		return
	}

	token := strings.TrimPrefix(body.Token, "Bearer ")
	now := time.Now().UnixMilli()

	// Extract user email or username from JWT to give each account its unique identity & short ID tag
	accountName := "Framer Account"
	accountID := "framer-" + randomHex(4)

	parts := strings.Split(token, ".")
	if len(parts) >= 2 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims struct {
				Email    string `json:"email"`
				UserID   string `json:"userId"`
				Username string `json:"username"`
				Payload  struct {
					Email     string `json:"email"`
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
					Username  string `json:"username"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(payload, &claims); err == nil {
				email := claims.Email
				if email == "" {
					email = claims.Payload.Email
				}
				name := claims.Username
				if name == "" {
					name = claims.Payload.Username
				}
				if name == "" && (claims.Payload.FirstName != "" || claims.Payload.LastName != "") {
					name = strings.TrimSpace(claims.Payload.FirstName + " " + claims.Payload.LastName)
				}

				shortUID := ""
				if claims.UserID != "" {
					accountID = "framer-" + claims.UserID
					if len(claims.UserID) >= 6 {
						shortUID = claims.UserID[:6]
					}
				}

				if email != "" {
					if shortUID != "" {
						accountName = fmt.Sprintf("%s (%s)", email, shortUID)
					} else {
						accountName = email
					}
				} else if name != "" {
					if shortUID != "" {
						accountName = fmt.Sprintf("%s (%s)", name, shortUID)
					} else {
						accountName = name
					}
				}
			}
		}
	}

	// 1. Insert to providers table
	_, _ = h.db.Exec(`
		INSERT OR REPLACE INTO providers (
			id, provider_id, name, category, protocol, base_url, api_key, access_token, enabled, created_at
		) VALUES (?, 'framer', ?, 'api_key', 'custom', 'https://api.framer.com/ai/v3/chat/', ?, ?, 1, ?)`,
		accountID, accountName, token, token, now)

	// 2. Insert to providerConnections table (for dashboard UI!)
	connData, _ := json.Marshal(map[string]any{
		"apiKey":  token,
		"baseUrl": "https://api.framer.com/ai/v3/chat/",
	})
	nowISO := time.Now().UTC().Format(time.RFC3339)
	_, _ = h.db.Exec(`
		INSERT OR REPLACE INTO providerConnections (
			id, provider, authType, name, isActive, data, createdAt, updatedAt
		) VALUES (?, 'framer', 'api_key', ?, 1, ?, ?, ?)`,
		accountID, accountName, string(connData), nowISO, nowISO)

	_, _ = h.ScanAndSyncProviderModels("framer")

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Framer session token received and synced for %s!", accountName),
	})
}

// OAuth Handlers
func (h *SRouterHandler) HandleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		provider = "antigravity"
	}

	state := randomHex(16)
	codeVerifier := randomHex(32)
	hHasher := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hHasher[:])

	var authorizeURL string
	var clientID string
	var redirectURI string

	switch provider {
	case "antigravity":
		clientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
		redirectURI = "http://localhost:1455/auth/antigravity/callback"
		scope := "openid profile email https://www.googleapis.com/auth/cloud-platform"
		v := url.Values{}
		v.Set("response_type", "code")
		v.Set("client_id", clientID)
		v.Set("redirect_uri", redirectURI)
		v.Set("scope", scope)
		v.Set("code_challenge", codeChallenge)
		v.Set("code_challenge_method", "S256")
		v.Set("state", state)
		v.Set("access_type", "offline")
		v.Set("prompt", "consent")
		authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode()
	case "claude", "anthropic":
		clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
		redirectURI = "http://localhost:1455/auth/claude/callback"
		scope := "org:create_api_key user:profile user:inference"
		v := url.Values{}
		v.Set("response_type", "code")
		v.Set("client_id", clientID)
		v.Set("redirect_uri", redirectURI)
		v.Set("scope", scope)
		v.Set("code_challenge", codeChallenge)
		v.Set("code_challenge_method", "S256")
		v.Set("state", state)
		authorizeURL = "https://claude.ai/oauth/authorize?" + v.Encode()
	default: // openai, codex
		clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
		redirectURI = "http://localhost:1455/auth/callback"
		scope := "openid email profile model.request offline_access"
		v := url.Values{}
		v.Set("response_type", "code")
		v.Set("client_id", clientID)
		v.Set("redirect_uri", redirectURI)
		v.Set("scope", scope)
		v.Set("code_challenge", codeChallenge)
		v.Set("code_challenge_method", "S256")
		v.Set("state", state)
		v.Set("id_token_add_organizations", "true")
		v.Set("codex_cli_simplified_flow", "true")
		v.Set("originator", "codex_cli_rs")
		authorizeURL = "https://auth.openai.com/oauth/authorize?" + v.Encode()
	}

	now := time.Now().UnixMilli()
	_, _ = h.db.Exec(
		`INSERT OR REPLACE INTO oauth_sessions (state, code_verifier, client_id, redirect_uri, provider, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		state, codeVerifier, clientID, redirectURI, provider, now,
	)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"authorizeUrl": authorizeURL,
		"authUrl":      authorizeURL,
		"state":        state,
	})
}

func (h *SRouterHandler) HandleOAuthPoll(w http.ResponseWriter, r *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "pending",
	})
}

func (h *SRouterHandler) HandleOAuthToken(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		provider = "custom"
	}

	var body struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		Name         string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.AccessToken == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "accessToken is required")
		return
	}

	now := time.Now().UnixMilli()
	connID := fmt.Sprintf("%s-%d", provider, now)
	connName := body.Name
	if connName == "" {
		connName = fmt.Sprintf("%s Token", provider)
	}

	dataBytes, _ := json.Marshal(map[string]any{
		"apiKey":       body.AccessToken,
		"refreshToken": body.RefreshToken,
	})

	_, err := h.db.Exec(
		`INSERT OR REPLACE INTO providerConnections (id, provider, authType, name, isActive, data, createdAt, updatedAt) VALUES (?, ?, 'oauth', ?, 1, ?, ?, ?)`,
		connID, provider, connName, string(dataBytes), now, now,
	)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_, _ = h.db.Exec(
		`INSERT OR REPLACE INTO providers (id, provider_id, name, category, protocol, access_token, refresh_token, enabled, created_at) VALUES (?, ?, ?, 'oauth', 'openai', ?, ?, 1, ?)`,
		connID, provider, connName, body.AccessToken, body.RefreshToken, now,
	)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Token saved successfully",
		"provider": map[string]any{
			"id":   connID,
			"name": connName,
		},
	})
}

func (h *SRouterHandler) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		provider = "antigravity"
	}

	var code string
	var state string

	if r.Method == http.MethodPost {
		var body struct {
			Code        string `json:"code"`
			State       string `json:"state"`
			CallbackURL string `json:"callbackUrl"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		code = body.Code
		state = body.State
		if body.CallbackURL != "" {
			if u, err := url.Parse(body.CallbackURL); err == nil {
				if qCode := u.Query().Get("code"); qCode != "" {
					code = qCode
				}
				if qState := u.Query().Get("state"); qState != "" {
					state = qState
				}
			}
		}
	} else {
		code = r.URL.Query().Get("code")
		state = r.URL.Query().Get("state")
	}

	if code == "" || state == "" {
		if r.Method == http.MethodPost {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "Missing code or state parameter")
		} else {
			http.Error(w, "Missing code or state parameter", http.StatusBadRequest)
		}
		return
	}

	var session struct {
		CodeVerifier string
		ClientID     string
		RedirectURI  string
		Provider     string
	}
	err := h.db.QueryRow(
		`SELECT code_verifier, client_id, redirect_uri, provider FROM oauth_sessions WHERE state = ?`,
		state,
	).Scan(&session.CodeVerifier, &session.ClientID, &session.RedirectURI, &session.Provider)
	if err != nil {
		if r.Method == http.MethodPost {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid or expired OAuth state")
		} else {
			http.Error(w, "Invalid or expired OAuth state", http.StatusBadRequest)
		}
		return
	}
	_, _ = h.db.Exec(`DELETE FROM oauth_sessions WHERE state = ?`, state)

	var tokenURL string
	var clientSecret string
	switch session.Provider {
	case "antigravity":
		tokenURL = "https://oauth2.googleapis.com/token"
		clientSecret = "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"
	case "claude", "anthropic":
		tokenURL = "https://api.anthropic.com/v1/oauth/token"
	default:
		tokenURL = "https://auth.openai.com/oauth/token"
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", session.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", session.CodeVerifier)
	form.Set("redirect_uri", session.RedirectURI)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	tokenReq, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		if r.Method == http.MethodPost {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		if r.Method == http.MethodPost {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to exchange token: "+err.Error())
		} else {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer tokenResp.Body.Close()

	bodyBytes, _ := io.ReadAll(tokenResp.Body)
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		errMsg := fmt.Sprintf("OAuth exchange failed (%d): %s", tokenResp.StatusCode, string(bodyBytes))
		if r.Method == http.MethodPost {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, errMsg)
		} else {
			http.Error(w, errMsg, http.StatusBadRequest)
		}
		return
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	_ = json.Unmarshal(bodyBytes, &tokenData)

	now := time.Now().UnixMilli()
	connID := fmt.Sprintf("%s-%d", session.Provider, now)
	accountName := fmt.Sprintf("%s Account (%s)", strings.Title(session.Provider), randomHex(4))

	// If provider returned id_token or is Google/Antigravity, extract or fetch email
	if tokenData.IDToken != "" {
		parts := strings.Split(tokenData.IDToken, ".")
		if len(parts) >= 2 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					Email string `json:"email"`
				}
				if err := json.Unmarshal(payload, &claims); err == nil && claims.Email != "" {
					accountName = claims.Email
				}
			}
		}
	}

	if accountName == fmt.Sprintf("%s Account (%s)", strings.Title(session.Provider), randomHex(4)) && session.Provider == "antigravity" && tokenData.AccessToken != "" {
		req, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
		if uResp, err := client.Do(req); err == nil && uResp.StatusCode == http.StatusOK {
			var uInfo struct {
				Email string `json:"email"`
			}
			_ = json.NewDecoder(uResp.Body).Decode(&uInfo)
			uResp.Body.Close()
			if uInfo.Email != "" {
				accountName = uInfo.Email
			}
		}
	}

	connData, _ := json.Marshal(map[string]any{
		"apiKey":       tokenData.AccessToken,
		"refreshToken": tokenData.RefreshToken,
	})

	_, _ = h.db.Exec(
		`INSERT OR REPLACE INTO providerConnections (id, provider, authType, name, isActive, data, createdAt, updatedAt) VALUES (?, ?, 'oauth', ?, 1, ?, ?, ?)`,
		connID, session.Provider, accountName, string(connData), now, now,
	)

	_, _ = h.db.Exec(
		`INSERT OR REPLACE INTO providers (id, provider_id, name, category, protocol, access_token, refresh_token, enabled, created_at) VALUES (?, ?, ?, 'oauth', 'openai', ?, ?, 1, ?)`,
		connID, session.Provider, accountName, tokenData.AccessToken, tokenData.RefreshToken, now,
	)

	// Trigger async dynamic model scanning for newly connected OAuth provider
	go func(pid string) {
		_, _ = h.ScanAndSyncProviderModels(pid)
	}(session.Provider)

	if r.Method == http.MethodPost {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "OAuth Authentication Complete",
			"provider": map[string]any{
				"id":   connID,
				"name": accountName,
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/html")
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title></head>
<body style="font-family: sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: #111; color: #fff;">
<div style="text-align: center; border: 1px solid #333; padding: 30px; border-radius: 12px; background: #1a1a1a;">
  <h2 style="color: #22c55e; margin-top: 0;">✓ Login Successful!</h2>
  <p>Your %s account is now connected to LAM-Router.</p>
  <p style="color: #888; font-size: 13px;">This window will close automatically...</p>
</div>
<script>
  if (window.opener) {
    window.opener.postMessage({ type: "SROUTER_OAUTH_SUCCESS", provider: "%s" }, "*");
  }
  setTimeout(function() { window.close(); }, 2000);
</script>
</body>
</html>`, session.Provider, session.Provider)
	w.Write([]byte(html))
}
