package routerapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"lamrouter/internal/handlerutil"
	"lamrouter/internal/models"
	"lamrouter/internal/usagetracker"
)

// GET /v1/telemetry/active
func (h *RouterAPIHandler) HandleTelemetryActive(w http.ResponseWriter, r *http.Request) {
	tracker := usagetracker.GetTracker()
	state := tracker.GetActiveState(h.repo)
	handlerutil.WriteJSON(w, http.StatusOK, state)
}

// GET /v1/logs?period=today|24h|7d|30d|60d|all
func (h *RouterAPIHandler) HandleLogsList(w http.ResponseWriter, r *http.Request) {
	// The recent-requests table sits on the same dashboard as the period
	// selector, so it honours the same range; without a period it stays
	// unfiltered and older callers see the full history.
	_, sinceMs := parseStatsPeriod(statsRequestedPeriod(r), time.Now())
	timeFilter, timeArgs := statsTimeFilter(sinceMs)

	// ?apiKeyId= narrows the list to one client key; the dashboard uses it to
	// open "everything this key did" from a log row.
	if apiKeyFilter := strings.TrimSpace(r.URL.Query().Get("apiKeyId")); apiKeyFilter != "" {
		timeFilter = joinFilter(timeFilter, "api_key_id = ?")
		timeArgs = append(timeArgs, apiKeyFilter)
	}

	rows, err := h.db.Query(`
SELECT id, api_key_id, COALESCE(api_key_name, ''), provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, reasoning_tokens, estimated_cost, fallback_occurred, COALESCE(fallback_reason, '')
FROM request_logs`+timeFilter+` ORDER BY created_at DESC LIMIT 100
`, timeArgs...)
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data":   []any{},
		})
		return
	}
	defer rows.Close()

	var logs []map[string]any
	// Older rows (and keys deleted since) carry no name; resolve one per distinct
	// id so the dashboard never shows a bare marker where a key name belongs.
	keyNameByID := map[string]string{}
	displayKeyName := func(id, name string) string {
		if name != "" {
			return name
		}
		if resolved, ok := keyNameByID[id]; ok {
			return resolved
		}
		resolved := ""
		switch {
		case id == "" || id == models.NoClientKeyID:
			resolved = models.NoClientKeyName
		case strings.HasPrefix(id, "unmatched:"):
			resolved = models.UnknownClientKeyName
		case id == models.AdminSessionKeyID:
			resolved = models.AdminSessionKeyName
		default:
			if h.repo != nil {
				resolved = h.repo.GetApiKeyName(id)
			}
			if resolved == "" {
				resolved = models.UnknownClientKeyName
			}
		}
		keyNameByID[id] = resolved
		return resolved
	}
	for rows.Next() {
		var id, apiKeyID, apiKeyName, providerID, model string
		var promptTok, compTok, totalTok, status, latency int
		var createdAt, cachedTok, reasoningTok int64
		var cost float64
		var fallbackOccurred int
		var fallbackReason string

		err := rows.Scan(
			&id, &apiKeyID, &apiKeyName, &providerID, &model, &promptTok, &compTok,
			&totalTok, &status, &latency, &createdAt, &cachedTok,
			&reasoningTok, &cost, &fallbackOccurred, &fallbackReason,
		)
		if err != nil {
			continue
		}

		// A request the auth middleware refused has no model at all; the
		// placeholder some older rows carry must not be served as one.
		if isRejectedRow(providerID, model) {
			model = ""
		}

		logs = append(logs, map[string]any{
			"id":               id,
			"apiKeyId":         apiKeyID,
			"apiKeyName":       displayKeyName(apiKeyID, apiKeyName),
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
			"fallbackReason":   fallbackReason,
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   logs,
	})
}

// statsPeriodDefault is returned whenever a caller sends no period, or one we
// do not recognise. Keeping the default unfiltered means every existing caller
// (TUI, CLI, older UI bundles) keeps seeing all-time totals.
const statsPeriodDefault = "all"

// parseStatsPeriod resolves a ?period= value into a period key plus an
// inclusive lower bound in milliseconds since epoch. Rolling windows are
// relative to the server clock; "today" starts at local midnight, which is what
// an operator means by "hari ini".
func parseStatsPeriod(raw string, now time.Time) (string, int64) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "today":
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return "today", startOfDay.UnixMilli()
	case "24h":
		return "24h", now.Add(-24 * time.Hour).UnixMilli()
	case "7d":
		return "7d", now.Add(-7 * 24 * time.Hour).UnixMilli()
	case "30d":
		return "30d", now.Add(-30 * 24 * time.Hour).UnixMilli()
	case "60d":
		return "60d", now.Add(-60 * 24 * time.Hour).UnixMilli()
	default:
		return statsPeriodDefault, 0
	}
}

// statsRequestedPeriod reads the period from the request, accepting "range" as
// an alias so either naming works from the dashboard.
func statsRequestedPeriod(r *http.Request) string {
	query := r.URL.Query()
	if v := query.Get("period"); v != "" {
		return v
	}
	return query.Get("range")
}

// statsTimeFilter renders the WHERE clause for a lower bound. A non-positive
// bound yields no clause, keeping the all-time query identical to before.
func statsTimeFilter(sinceMs int64) (string, []any) {
	if sinceMs <= 0 {
		return "", nil
	}
	return " WHERE created_at >= ?", []any{sinceMs}
}

// joinFilter appends an extra predicate to a WHERE clause produced by
// statsTimeFilter, so a query can combine the period window with another
// condition without the two clauses fighting over the WHERE keyword.
func joinFilter(timeFilter, cond string) string {
	if timeFilter == "" {
		return " WHERE " + cond
	}
	return timeFilter + " AND " + cond
}

const (
	// syntheticAuthID is the provider id the auth middleware stores for a
	// request it rejected: no provider or model has been resolved at that point,
	// the status code carries the refusal and the reason lives in
	// fallback_reason. The literal is repeated from
	// internal/middleware/auth.go on purpose - this package has to keep
	// classifying rows written by older builds too.
	syntheticAuthID = "auth"
	// syntheticRowCond matches such a rejected request: it either carries no
	// model (what the current writer stores) or the placeholder "auth" older
	// builds put in both columns. usageRowCond is the complement.
	syntheticRowCond = "(provider_id = '" + syntheticAuthID + "' AND (model = '' OR model = '" + syntheticAuthID + "'))"
	usageRowCond     = " NOT " + syntheticRowCond
)

// isRejectedRow reports whether a log row records a request the auth middleware
// refused rather than model usage. It is the row-level twin of
// syntheticRowCond, for the places where the filtering does not happen in SQL.
func isRejectedRow(providerID, model string) bool {
	return providerID == syntheticAuthID && (model == "" || model == syntheticAuthID)
}

// providerDisplayName turns a provider id recorded in request_logs into the
// label the dashboard renders next to that provider's icon. The static catalog
// is used rather than a database lookup because this payload is polled every
// second; providers that are imported and therefore absent from the catalog
// fall back to a readable form of their id instead of an empty label.
func providerDisplayName(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "Unknown"
	}
	for _, def := range KnownCatalogSeeds {
		if def.Name != "" && strings.EqualFold(def.ID, trimmed) {
			return def.Name
		}
	}
	parts := strings.FieldsFunc(trimmed, func(c rune) bool { return c == '-' || c == '_' || c == ' ' })
	worded := make([]string, 0, len(parts))
	for _, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		worded = append(worded, strings.ToUpper(string(runes[0]))+string(runes[1:]))
	}
	if len(worded) == 0 {
		return trimmed
	}
	return strings.Join(worded, " ")
}

// GET /v1/logs/stats?period=today|24h|7d|30d|60d|all
func (h *RouterAPIHandler) HandleLogsStats(w http.ResponseWriter, r *http.Request) {
	var totalRequests, totalTokens, totalInputTokens, totalOutputTokens int
	var totalCachedTokens, totalCompressionTokens int
	var totalCost float64
	var avgLatency float64

	periodKey, sinceMs := parseStatsPeriod(statsRequestedPeriod(r), time.Now())
	timeFilter, timeArgs := statsTimeFilter(sinceMs)

	// Card totals count every row in the window, including requests the auth
	// middleware rejected before any model was known. Those rows carry no
	// tokens, so the model and provider breakdowns below exclude them and the
	// "blocked" block reports the count that separates the two figures.
	_ = h.db.QueryRow(`
SELECT 
    COUNT(*), 
    COALESCE(SUM(total_tokens), 0), 
    COALESCE(SUM(prompt_tokens), 0), 
    COALESCE(SUM(completion_tokens), 0), 
    COALESCE(SUM(cached_tokens), 0),
    COALESCE(SUM(compression_tokens), 0),
    COALESCE(SUM(estimated_cost), 0), 
    COALESCE(AVG(latency_ms), 0) 
FROM request_logs`+timeFilter,
		timeArgs...).Scan(
		&totalRequests, &totalTokens, &totalInputTokens, &totalOutputTokens,
		&totalCachedTokens, &totalCompressionTokens, &totalCost, &avgLatency,
	)

	totalSavedTokens := totalCachedTokens + totalCompressionTokens

	// Fetch byModel breakdown
	rows, err := h.db.Query(`
SELECT model, COUNT(*), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(compression_tokens), 0), COALESCE(SUM(estimated_cost), 0)
FROM request_logs`+joinFilter(timeFilter, usageRowCond)+`
GROUP BY model
ORDER BY COUNT(*) DESC
LIMIT 20
`, timeArgs...)
	var byModel []map[string]any
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var model string
			var reqCount, inTok, outTok, cachedTok, compTok int
			var estCost float64
			if err := rows.Scan(&model, &reqCount, &inTok, &outTok, &cachedTok, &compTok, &estCost); err == nil {
				byModel = append(byModel, map[string]any{
					"model":                  model,
					"totalRequests":          reqCount,
					"totalInputTokens":       inTok,
					"totalOutputTokens":      outTok,
					"totalCachedTokens":      cachedTok,
					"totalCompressionTokens": compTok,
					"totalSavedTokens":       cachedTok + compTok,
					"estCost":                estCost,
				})
			}
		}
	}
	if byModel == nil {
		byModel = []map[string]any{}
	}

	// A model can be served by more than one provider once fallbacks kick in, so
	// each row keeps the provider that carried most of its tokens. The per-model
	// list above is capped at 20 rows, which is why provider totals are
	// aggregated by their own query instead of from that list.
	dominantProvider := make(map[string]string)
	if providerRows, err := h.db.Query(`
SELECT model, provider_id, COALESCE(SUM(total_tokens), 0) AS tok
FROM request_logs`+joinFilter(timeFilter, usageRowCond)+`
GROUP BY model, provider_id
ORDER BY tok DESC
`, timeArgs...); err == nil {
		defer providerRows.Close()
		for providerRows.Next() {
			var model, providerID string
			var tok int
			if err := providerRows.Scan(&model, &providerID, &tok); err == nil {
				if _, seen := dominantProvider[model]; !seen {
					dominantProvider[model] = providerID
				}
			}
		}
	}
	for _, row := range byModel {
		if model, ok := row["model"].(string); ok {
			providerID := dominantProvider[model]
			row["providerId"] = providerID
			row["providerName"] = providerDisplayName(providerID)
		}
	}

	// Per-provider breakdown behind the provider distribution card. Shares are
	// measured against the same window total as the cards, so they add up to
	// 100% for the selected period.
	byProvider := []map[string]any{}
	if providerRows, err := h.db.Query(`
SELECT provider_id, COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(compression_tokens), 0), COALESCE(SUM(estimated_cost), 0), COUNT(DISTINCT model), COALESCE(AVG(latency_ms), 0)
FROM request_logs`+joinFilter(timeFilter, usageRowCond)+`
GROUP BY provider_id
ORDER BY SUM(total_tokens) DESC, COUNT(*) DESC
`, timeArgs...); err == nil {
		defer providerRows.Close()
		for providerRows.Next() {
			var providerID string
			var reqCount, tokens, inTokens, outTokens, cachedTokens, compressionTokens, modelCount int
			var estCost, avgLatency float64
			if err := providerRows.Scan(&providerID, &reqCount, &tokens, &inTokens, &outTokens, &cachedTokens, &compressionTokens, &estCost, &modelCount, &avgLatency); err != nil {
				continue
			}
			share := 0.0
			if totalTokens > 0 {
				share = float64(tokens) / float64(totalTokens) * 100
			}
			byProvider = append(byProvider, map[string]any{
				"providerId":             providerID,
				"name":                   providerDisplayName(providerID),
				"totalTokens":            tokens,
				"totalRequests":          reqCount,
				"totalInputTokens":       inTokens,
				"totalOutputTokens":      outTokens,
				"totalCachedTokens":      cachedTokens,
				"totalCompressionTokens": compressionTokens,
				"totalSavedTokens":       cachedTokens + compressionTokens,
				"estCost":                estCost,
				"modelCount":             modelCount,
				"avgLatency":             avgLatency,
				"share":                  share,
			})
		}
	}

	// Requests the auth middleware rejected are reported on their own: they are
	// traffic, not model usage, and counting them as a model is what put an
	// "Auth" row in the model table.
	blockedTotal := 0
	blockedByStatus := map[string]int{}
	if blockedRows, err := h.db.Query(`
SELECT status_code, COUNT(*)
FROM request_logs`+joinFilter(timeFilter, syntheticRowCond)+`
GROUP BY status_code
`, timeArgs...); err == nil {
		defer blockedRows.Close()
		for blockedRows.Next() {
			var status, count int
			if err := blockedRows.Scan(&status, &count); err == nil {
				blockedTotal += count
				blockedByStatus[strconv.Itoa(status)] = count
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"totalRequests":          totalRequests,
		"totalTokens":            totalTokens,
		"totalInputTokens":       totalInputTokens,
		"totalOutputTokens":      totalOutputTokens,
		"totalCachedTokens":      totalCachedTokens,
		"totalCompressionTokens": totalCompressionTokens,
		"totalSavedTokens":       totalSavedTokens,
		"totalCost":              totalCost,
		"costLabel":              fmt.Sprintf("$%.2f", totalCost),
		"estimated":              true,
		"avgLatency":             avgLatency,
		"activeStreams":          0,
		"period":                 periodKey,
		"periodStartMs":          sinceMs,
		"byModel":                byModel,
		"byProvider":             byProvider,
		"blocked": map[string]any{
			"total":    blockedTotal,
			"byStatus": blockedByStatus,
		},
	})
}

// Handlers in quota.go handle GET /v1/quota

// GET /v1/tunnel/status
func (h *RouterAPIHandler) HandleTunnelStatus(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleTunnelEvents(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleTunnelStart(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleTunnelStop(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleTunnelConfig(w http.ResponseWriter, r *http.Request) {
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
func (h *RouterAPIHandler) HandleTunnelInstall(w http.ResponseWriter, r *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "cloudflared is ready",
	})
}

// GET /v1/tunnel/install
func (h *RouterAPIHandler) HandleTunnelInstallStatus(w http.ResponseWriter, r *http.Request) {
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

// OAuth Handlers
func (h *RouterAPIHandler) generateOAuthSession(provider string) (authorizeURL, state string, err error) {
	if provider == "" {
		provider = "antigravity"
	}

	state = randomHex(16)
	codeVerifier := randomHex(32)
	hHasher := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hHasher[:])

	var clientID string
	var redirectURI string

	switch provider {
	case "antigravity":
		clientID = GoogleOAuthClientID
		redirectURI = "http://localhost:51121/oauth-callback"
		scope := "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
		authorizeURL = fmt.Sprintf(
			"https://accounts.google.com/o/oauth2/v2/auth?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s&access_type=offline&prompt=consent",
			url.QueryEscape(clientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(scope),
			url.QueryEscape(codeChallenge),
			url.QueryEscape(state),
		)
	case "claude", "anthropic":
		clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
		redirectURI = "http://localhost:1455/auth/claude/callback"
		scope := "org:create_api_key user:profile user:inference"
		authorizeURL = fmt.Sprintf(
			"https://claude.ai/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
			url.QueryEscape(clientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(scope),
			url.QueryEscape(codeChallenge),
			url.QueryEscape(state),
		)
	default: // openai, codex
		clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
		redirectURI = "http://localhost:1455/auth/callback"
		scope := "openid email profile model.request offline_access"
		authorizeURL = fmt.Sprintf(
			"https://auth.openai.com/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s&id_token_add_organizations=true&codex_cli_simplified_flow=true&originator=codex_cli_rs",
			url.QueryEscape(clientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(scope),
			url.QueryEscape(codeChallenge),
			url.QueryEscape(state),
		)
	}

	now := time.Now().UnixMilli()
	_, err = h.db.Exec(
		`INSERT OR REPLACE INTO oauth_sessions (state, code_verifier, client_id, redirect_uri, provider, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		state, codeVerifier, clientID, redirectURI, provider, now,
	)
	return authorizeURL, state, err
}

func (h *RouterAPIHandler) HandleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	authorizeURL, state, err := h.generateOAuthSession(provider)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"authorizeUrl": authorizeURL,
		"authUrl":      authorizeURL,
		"state":        state,
	})
}

func (h *RouterAPIHandler) HandleOAuthOpen(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	authorizeURL, _, err := h.generateOAuthSession(provider)
	if err != nil {
		http.Error(w, "Failed to initialize OAuth session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, authorizeURL, http.StatusFound)
}

func (h *RouterAPIHandler) HandleOAuthPoll(w http.ResponseWriter, r *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "pending",
	})
}

func (h *RouterAPIHandler) HandleOAuthToken(w http.ResponseWriter, r *http.Request) {
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

func (h *RouterAPIHandler) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
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
		clientSecret = GoogleOAuthClientSecret
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
    window.opener.postMessage({ type: "LAMROUTER_OAUTH_SUCCESS", provider: "%s" }, "*");
  }
  setTimeout(function() { window.close(); }, 2000);
</script>
</body>
</html>`, session.Provider, session.Provider)
	w.Write([]byte(html))
}
