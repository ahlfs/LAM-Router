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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Antigravity/1.0 (VSCode)")
	req.Header.Set("x-goog-api-client", "gl-node/18.0.0 gd/1.0.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var data CloudCodeFetchAvailableModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if len(data.Models) == 0 {
		return nil, fmt.Errorf("no models in response")
	}

	var quotas []LiveModelQuotaItem
	for modelID, item := range data.Models {
		// Filter out internal IDE autocomplete models, test checkpoints, and non-chat models
		lowerID := strings.ToLower(modelID)
		lowerName := strings.ToLower(item.DisplayName)
		if strings.HasPrefix(lowerID, "tab_") || strings.Contains(lowerID, "tab_flash") || strings.Contains(lowerID, "tab_jump") ||
			strings.HasPrefix(lowerID, "chat_") || strings.Contains(lowerID, "-tiered") ||
			strings.HasPrefix(lowerName, "tab_") || strings.HasPrefix(lowerName, "chat_") || strings.Contains(lowerName, "tiered") {
			continue
		}

		remFraction := 1.0
		var resetTime string
		if item.QuotaInfo != nil {
			remFraction = item.QuotaInfo.RemainingFraction
			resetTime = item.QuotaInfo.ResetTime
		}

		pctValue := int(remFraction * 100)
		if pctValue > 100 {
			pctValue = 100
		}
		if pctValue < 0 {
			pctValue = 0
		}

		limit := 1000
		used := int(float64(limit) * (1.0 - remFraction))
		if used < 0 {
			used = 0
		}

		status := "ok"
		if pctValue <= 5 {
			status = "exhausted"
		} else if pctValue <= 20 {
			status = "warning"
		}

		name := item.DisplayName
		if name == "" {
			name = modelID
		}

		quotas = append(quotas, LiveModelQuotaItem{
			Name:            name,
			Used:            used,
			Limit:           limit,
			Percentage:      fmt.Sprintf("%d%%", pctValue),
			PercentageValue: pctValue,
			ResetIn:         formatResetIn(resetTime),
			ResetTime:       resetTime,
			Status:          status,
		})
	}

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
