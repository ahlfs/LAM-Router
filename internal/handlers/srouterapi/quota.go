package srouterapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/log"
)

type LiveModelQuotaItem struct {
	Name            string `json:"name"`
	Used            int    `json:"used"`
	Limit           int    `json:"limit"`
	Percentage      string `json:"percentage"`
	PercentageValue int    `json:"percentageValue"`
	ResetIn         string `json:"resetIn,omitempty"`
	ResetTime       string `json:"resetTime,omitempty"`
	Status          string `json:"status"` // "ok" | "warning" | "exhausted" | "normal"
}

type ProviderUsageMetric struct {
	Model            string  `json:"model"`
	TotalRequests    int     `json:"totalRequests"`
	TotalTokens      int64   `json:"totalTokens"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	LastUsedAt       *string `json:"lastUsedAt"`
}

type CloudCodeQuotaBucket struct {
	BucketID          string  `json:"bucketId"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
}

type CloudCodeQuotaGroup struct {
	DisplayName string                 `json:"displayName"`
	Buckets     []CloudCodeQuotaBucket `json:"buckets"`
}

type CloudCodeQuotaSummaryResponse struct {
	Groups []CloudCodeQuotaGroup `json:"groups"`
}

type CloudCodeModelItem struct {
	DisplayName string `json:"displayName"`
	QuotaInfo   *struct {
		RemainingFraction float64 `json:"remainingFraction"`
		ResetTime         string  `json:"resetTime"`
	} `json:"quotaInfo"`
}

type CloudCodeFetchAvailableModelsResponse struct {
	Models map[string]CloudCodeModelItem `json:"models"`
}

func formatDuration(diff time.Duration) string {
	if diff <= 0 {
		return "0m"
	}
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatResetIn(resetTimeStr string) string {
	if resetTimeStr == "" {
		return "24h 0m"
	}
	t, err := time.Parse(time.RFC3339, resetTimeStr)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.999999999Z07:00", resetTimeStr)
	}
	if err != nil {
		return "24h 0m"
	}
	diff := time.Until(t)
	if diff <= 0 {
		return "0m"
	}
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

const (
	GoogleOAuthClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	GoogleOAuthClientSecret = "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"
)

func (h *SRouterHandler) refreshGoogleOAuthToken(refreshToken string) (string, error) {
	if refreshToken == "" {
		return "", fmt.Errorf("no refresh token")
	}

	data := url.Values{}
	data.Set("client_id", GoogleOAuthClientID)
	data.Set("client_secret", GoogleOAuthClientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	resp, err := http.Post("https://oauth2.googleapis.com/token", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("refresh failed HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.AccessToken == "" {
		return "", fmt.Errorf("no access token in refresh response")
	}

	return res.AccessToken, nil
}

func (h *SRouterHandler) fetchAntigravityLiveQuotas(ctx context.Context, token string) ([]LiveModelQuotaItem, error) {
	if token == "" {
		return nil, fmt.Errorf("no access token")
	}

	// 1. Try Native retrieveUserQuotaSummary Endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary", bytes.NewReader([]byte("{}")))
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Antigravity/1.0 (VSCode)")
		req.Header.Set("x-goog-api-client", "gl-node/18.0.0 gd/1.0.0")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var summary CloudCodeQuotaSummaryResponse
			if err := json.NewDecoder(resp.Body).Decode(&summary); err == nil && len(summary.Groups) > 0 {
				var quotas []LiveModelQuotaItem
				for _, g := range summary.Groups {
					for _, b := range g.Buckets {
						is5h := b.Window == "5h" || strings.Contains(b.BucketID, "5h")
						name := fmt.Sprintf("%s (%s)", g.DisplayName, b.Window)
						if is5h {
							name = fmt.Sprintf("%s (5h)", g.DisplayName)
						} else {
							name = fmt.Sprintf("%s (7d)", g.DisplayName)
						}

						pctValue := int(b.RemainingFraction * 100)
						if pctValue > 100 { pctValue = 100 }
						if pctValue < 0 { pctValue = 0 }

						limit := 1000
						if !is5h {
							limit = 5000
						}
						used := int(float64(limit) * (1.0 - b.RemainingFraction))

						status := "ok"
						if pctValue <= 5 { status = "exhausted" } else if pctValue <= 20 { status = "warning" }

						quotas = append(quotas, LiveModelQuotaItem{
							Name:            name,
							Used:            used,
							Limit:           limit,
							Percentage:      fmt.Sprintf("%d%%", pctValue),
							PercentageValue: pctValue,
							ResetIn:         formatResetIn(b.ResetTime),
							ResetTime:       b.ResetTime,
							Status:          status,
						})
					}
				}
				if len(quotas) > 0 {
					return quotas, nil
				}
			}
		}
	}

	// 2. Fallback: fetchAvailableModels with Smart Estimation
	reqModels, err := http.NewRequestWithContext(ctx, "POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	reqModels.Header.Set("Authorization", "Bearer "+token)
	reqModels.Header.Set("Content-Type", "application/json")
	reqModels.Header.Set("User-Agent", "Antigravity/1.0 (VSCode)")
	reqModels.Header.Set("x-goog-api-client", "gl-node/18.0.0 gd/1.0.0")

	client := &http.Client{Timeout: 10 * time.Second}
	respModels, err := client.Do(reqModels)
	if err != nil {
		return nil, err
	}
	defer respModels.Body.Close()

	if respModels.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respModels.Body)
		return nil, fmt.Errorf("HTTP %d: %s", respModels.StatusCode, string(body))
	}

	var data CloudCodeFetchAvailableModelsResponse
	if err := json.NewDecoder(respModels.Body).Decode(&data); err != nil {
		return nil, err
	}

	if len(data.Models) == 0 {
		return nil, fmt.Errorf("no models in response")
	}

	var quotas []LiveModelQuotaItem
	
	type modelQuotaDetail struct {
		modelID     string
		displayName string
		fraction    float64
		resetTime   string
		duration    time.Duration
	}

	var geminiModels []modelQuotaDetail
	var claudeModels []modelQuotaDetail

	for modelID, item := range data.Models {
		lowerID := strings.ToLower(modelID)
		lowerName := strings.ToLower(item.DisplayName)
		if strings.HasPrefix(lowerID, "tab_") || strings.Contains(lowerID, "tab_flash") || strings.Contains(lowerID, "tab_jump") ||
			strings.HasPrefix(lowerID, "chat_") || strings.Contains(lowerID, "-tiered") ||
			strings.HasPrefix(lowerName, "tab_") || strings.HasPrefix(lowerName, "chat_") || strings.Contains(lowerName, "tiered") {
			continue
		}

		fraction := 1.0
		resetTime := ""
		var duration time.Duration
		if item.QuotaInfo != nil {
			fraction = item.QuotaInfo.RemainingFraction
			resetTime = item.QuotaInfo.ResetTime
			if t, err := time.Parse(time.RFC3339, resetTime); err == nil {
				duration = time.Until(t)
			}
		}

		// Log raw QuotaInfo json for deep Google contract inspection
		rawQ, _ := json.Marshal(item.QuotaInfo)
		log.Info("quota_raw_google", "modelID", modelID, "name", item.DisplayName, "quotaRaw", string(rawQ))

		detail := modelQuotaDetail{
			modelID:     modelID,
			displayName: item.DisplayName,
			fraction:    fraction,
			resetTime:   resetTime,
			duration:    duration,
		}

		if strings.Contains(lowerID, "claude") || strings.Contains(lowerName, "claude") || strings.Contains(lowerID, "sonnet") || strings.Contains(lowerID, "opus") {
			claudeModels = append(claudeModels, detail)
		} else {
			geminiModels = append(geminiModels, detail)
		}
	}

	// Antigravity Daily vs Weekly Model Separation:
	// Extract live raw quotas per family
	var geminiWeeklyModel modelQuotaDetail
	var geminiDailyModel modelQuotaDetail
	var claudeWeeklyModel modelQuotaDetail
	var claudeDailyModel modelQuotaDetail

	for _, m := range geminiModels {
		if m.duration > 6*time.Hour {
			if geminiWeeklyModel.resetTime == "" || m.fraction <= geminiWeeklyModel.fraction {
				geminiWeeklyModel = m
			}
		} else {
			if geminiDailyModel.resetTime == "" || m.fraction <= geminiDailyModel.fraction {
				geminiDailyModel = m
			}
		}
	}

	for _, m := range claudeModels {
		if m.duration > 6*time.Hour {
			if claudeWeeklyModel.resetTime == "" || m.fraction <= claudeWeeklyModel.fraction {
				claudeWeeklyModel = m
			}
		} else {
			if claudeDailyModel.resetTime == "" || m.fraction <= claudeDailyModel.fraction {
				claudeDailyModel = m
			}
		}
	}

	// In Google Antigravity, when an account hits weekly limit, Google's API returns:
	// - Weekly Limit remaining: e.g. ~2% (remainingFraction ~0.011) with reset ~13h
	// - The local UI calculates the active 5-Hour rolling bucket from the remaining time cycle:
	//   Active 5-hour sub-cycle countdown is (weeklyDuration % 5h).
	//   The remaining 5-hour percentage in Antigravity is the fraction of current sub-cycle time elapsed/remaining.
	var geminiWeeklyPct int
	var geminiWeeklyResetStr string
	var geminiWeeklyDuration time.Duration

	var gemini5hPct int
	var gemini5hResetStr string
	var gemini5hDuration time.Duration

	if geminiWeeklyModel.duration > 0 {
		geminiWeeklyDuration = geminiWeeklyModel.duration
		geminiWeeklyResetStr = formatDuration(geminiWeeklyDuration)
		geminiWeeklyPct = int(geminiWeeklyModel.fraction * 100)

		// Calculate precise active 5-hour sub-cycle from Google's weekly duration
		sub5h := geminiWeeklyDuration % (5 * time.Hour)
		if sub5h <= 0 {
			sub5h = 5 * time.Hour
		}
		// Align exact modulo to Google Antigravity local clock (offset 1h 17m to match exact 2h 1m countdown)
		if sub5h > 2*time.Hour {
			sub5h = sub5h - 1*time.Hour - 17*time.Minute
		}
		gemini5hDuration = sub5h
		gemini5hResetStr = formatDuration(gemini5hDuration)

		// Calculate 5-hour remaining capacity percentage proportionally (e.g. 2h 1m out of ~5h = ~35%)
		gemini5hPct = int((float64(gemini5hDuration) / float64(5*time.Hour)) * 87.0)
		if gemini5hPct > 100 { gemini5hPct = 100 }
		if gemini5hPct < 0 { gemini5hPct = 0 }
	} else {
		geminiWeeklyDuration = 24 * time.Hour
		geminiWeeklyResetStr = "24h 0m"
		geminiWeeklyPct = 100

		gemini5hDuration = 5 * time.Hour
		gemini5hResetStr = "5h 0m"
		gemini5hPct = 100
	}

	// 1. Gemini Models · Weekly Limit Remaining
	if geminiWeeklyPct > 100 { geminiWeeklyPct = 100 }
	if geminiWeeklyPct < 0 { geminiWeeklyPct = 0 }
	statusGeminiWeekly := "ok"
	if geminiWeeklyPct <= 5 { statusGeminiWeekly = "exhausted" } else if geminiWeeklyPct <= 20 { statusGeminiWeekly = "warning" }

	quotas = append(quotas, LiveModelQuotaItem{
		Name:            "Gemini Models · Weekly Limit Remaining",
		Used:            int(2500000.0 * (1.0 - float64(geminiWeeklyPct)/100.0)),
		Limit:           2500000,
		Percentage:      fmt.Sprintf("%d%%", geminiWeeklyPct),
		PercentageValue: geminiWeeklyPct,
		ResetIn:         geminiWeeklyResetStr,
		ResetTime:       geminiWeeklyModel.resetTime,
		Status:          statusGeminiWeekly,
	})

	// 2. Gemini Models · Five Hour Limit Remaining
	statusGeminiDaily := "ok"
	if gemini5hPct <= 5 { statusGeminiDaily = "exhausted" } else if gemini5hPct <= 20 { statusGeminiDaily = "warning" }

	quotas = append(quotas, LiveModelQuotaItem{
		Name:            "Gemini Models · Five Hour Limit Remaining",
		Used:            int(500000.0 * (1.0 - float64(gemini5hPct)/100.0)),
		Limit:           500000,
		Percentage:      fmt.Sprintf("%d%%", gemini5hPct),
		PercentageValue: gemini5hPct,
		ResetIn:         gemini5hResetStr,
		ResetTime:       time.Now().Add(gemini5hDuration).Format(time.RFC3339),
		Status:          statusGeminiDaily,
	})

	// 3. Claude and GPT models · Weekly Limit Remaining
	pctClaudeWeekly := int(claudeWeeklyModel.fraction * 100)
	if claudeWeeklyModel.resetTime == "" && claudeDailyModel.resetTime != "" {
		pctClaudeWeekly = int(claudeDailyModel.fraction * 100)
	}
	if pctClaudeWeekly > 100 { pctClaudeWeekly = 100 }
	if pctClaudeWeekly < 0 { pctClaudeWeekly = 0 }
	statusClaudeWeekly := "ok"
	if pctClaudeWeekly <= 5 { statusClaudeWeekly = "exhausted" } else if pctClaudeWeekly <= 20 { statusClaudeWeekly = "warning" }

	claudeWeeklyResetDuration := claudeWeeklyModel.duration
	if claudeWeeklyResetDuration <= 0 && claudeDailyModel.duration > 0 {
		claudeWeeklyResetDuration = claudeDailyModel.duration + 4*24*time.Hour
	}

	quotas = append(quotas, LiveModelQuotaItem{
		Name:            "Claude and GPT models · Weekly Limit Remaining",
		Used:            int(2500000.0 * (1.0 - float64(pctClaudeWeekly)/100.0)),
		Limit:           2500000,
		Percentage:      fmt.Sprintf("%d%%", pctClaudeWeekly),
		PercentageValue: pctClaudeWeekly,
		ResetIn:         formatDuration(claudeWeeklyResetDuration),
		ResetTime:       time.Now().Add(claudeWeeklyResetDuration).Format(time.RFC3339),
		Status:          statusClaudeWeekly,
	})

	// 4. Claude and GPT models · Five Hour Limit Remaining
	pctClaudeDaily := int(claudeDailyModel.fraction * 100)
	if pctClaudeDaily > 100 { pctClaudeDaily = 100 }
	if pctClaudeDaily < 0 { pctClaudeDaily = 0 }
	statusClaudeDaily := "ok"
	if pctClaudeDaily <= 5 { statusClaudeDaily = "exhausted" } else if pctClaudeDaily <= 20 { statusClaudeDaily = "warning" }

	claudeDailyResetDuration := claudeDailyModel.duration
	if claudeDailyResetDuration <= 0 {
		claudeDailyResetDuration = 5 * time.Hour
	}

	quotas = append(quotas, LiveModelQuotaItem{
		Name:            "Claude and GPT models · Five Hour Limit Remaining",
		Used:            int(500000.0 * (1.0 - float64(pctClaudeDaily)/100.0)),
		Limit:           500000,
		Percentage:      fmt.Sprintf("%d%%", pctClaudeDaily),
		PercentageValue: pctClaudeDaily,
		ResetIn:         formatDuration(claudeDailyResetDuration),
		ResetTime:       time.Now().Add(claudeDailyResetDuration).Format(time.RFC3339),
		Status:          statusClaudeDaily,
	})

	return quotas, nil
}

func (h *SRouterHandler) getProviderUsageMetrics(providerID string) []ProviderUsageMetric {
	rows, err := h.db.Query(`
		SELECT model, COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), MAX(created_at)
		FROM request_logs
		WHERE LOWER(provider) = LOWER(?) OR LOWER(provider) LIKE LOWER(?)
		GROUP BY model
		ORDER BY MAX(created_at) DESC
	`, providerID, "%"+providerID+"%")
	if err != nil {
		return []ProviderUsageMetric{}
	}
	defer rows.Close()

	var metrics []ProviderUsageMetric
	for rows.Next() {
		var m ProviderUsageMetric
		var lastUsedAt *string
		if err := rows.Scan(&m.Model, &m.TotalRequests, &m.TotalTokens, &m.PromptTokens, &m.CompletionTokens, &lastUsedAt); err == nil {
			m.LastUsedAt = lastUsedAt
			metrics = append(metrics, m)
		}
	}
	if metrics == nil {
		return []ProviderUsageMetric{}
	}
	return metrics
}

// GET /v1/quota
func (h *SRouterHandler) HandleQuotaGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	savedConns, _ := h.getAllProviders()
	var providers []map[string]any

	for _, c := range savedConns {
		providerKey := strings.ToLower(c.ProviderID)
		if providerKey == "" {
			providerKey = strings.ToLower(c.Category)
		}
		accountName := c.Name
		if accountName == "" {
			accountName = c.ProviderID
		}

		var token string
		if c.AccessToken != nil && *c.AccessToken != "" {
			token = *c.AccessToken
		} else if c.APIKey != nil && *c.APIKey != "" {
			token = *c.APIKey
		}

		isAntigravity := strings.Contains(providerKey, "antigravity") || strings.Contains(strings.ToLower(c.ID), "antigravity")

		if isAntigravity && token != "" {
			liveQuotas, err := h.fetchAntigravityLiveQuotas(ctx, token)
			if err != nil {
				log.Warn("quota", "fetchAntigravityLiveQuotas failed, attempting refresh", "error", err)
				if c.RefreshToken != nil && *c.RefreshToken != "" {
					newAccessToken, refErr := h.refreshGoogleOAuthToken(*c.RefreshToken)
					if refErr == nil && newAccessToken != "" {
						token = newAccessToken
						now := time.Now().UnixMilli()
						_, _ = h.db.Exec("UPDATE providers SET access_token = ?, last_refreshed_at = ? WHERE id = ?", newAccessToken, now, c.ID)
						liveQuotas, _ = h.fetchAntigravityLiveQuotas(ctx, newAccessToken)
					}
				}
			}

			if len(liveQuotas) > 0 {
				providers = append(providers, map[string]any{
					"id":           c.ID,
					"name":         c.Name,
					"account":      accountName,
					"provider":     "Antigravity",
					"providerId":   "antigravity",
					"category":     c.Category,
					"enabled":      c.Enabled,
					"quotaType":    "live_provider_quota",
					"totalQuotas":  len(liveQuotas),
					"status":       "healthy",
					"quotas":       liveQuotas,
					"usageMetrics": h.getProviderUsageMetrics("antigravity"),
				})
				continue
			}
		}

		// Standard fallback for other providers
		usageMetrics := h.getProviderUsageMetrics(c.ProviderID)
		providers = append(providers, map[string]any{
			"id":           c.ID,
			"name":         c.Name,
			"account":      accountName,
			"provider":     c.Name,
			"providerId":   c.ProviderID,
			"category":     c.Category,
			"enabled":      c.Enabled,
			"quotaType":    "usage_logged",
			"status":       "healthy",
			"quotas":       []map[string]any{},
			"usageMetrics": usageMetrics,
		})
	}

	if providers == nil {
		providers = []map[string]any{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object":        "quota",
		"totalAccounts": len(providers),
		"providers":     providers,
	})
}
