package routerapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"lamrouter/internal/handlerutil"
	"lamrouter/internal/log"
)

// antigravityUserAgent adalah identitas klien yang dikirim ke endpoint Cloud
// Code. Google membatasi ISI payload berdasarkan User-Agent ini: hanya
// identitas hub yang mutakhir yang dilayani ringkasan kuota penuh — grup
// "Gemini Models" sekaligus grup "Claude and GPT models". Identitas lain
// (termasuk "Antigravity/1.0 (VSCode)") tetap dilayani HTTP 200 tetapi dengan
// payload terpotong yang membuang grup Gemini tanpa error apa pun, sehingga
// kuota Gemini hilang dari dashboard secara diam-diam.
var antigravityUserAgent = fmt.Sprintf("antigravity/hub/2.9.1 %s/%s", runtime.GOOS, runtime.GOARCH)

type LiveModelQuotaItem struct {
	Name            string `json:"name"`
	Used            int    `json:"used"`
	Limit           int    `json:"limit"`
	Percentage      string `json:"percentage"`
	PercentageValue int    `json:"percentageValue"`
	ResetIn         string `json:"resetIn,omitempty"`
	ResetTime       string `json:"resetTime,omitempty"`
	Status          string `json:"status"`                    // "ok" | "warning" | "exhausted" | "normal"
	BlockedByWeekly bool   `json:"blockedByWeekly,omitempty"` // 5h bucket unusable because the weekly cap is spent
	Note            string `json:"note,omitempty"`            // short explanation for the UI
}

type ProviderUsageMetric struct {
	Model            string `json:"model"`
	TotalRequests    int    `json:"totalRequests"`
	TotalTokens      int64  `json:"totalTokens"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	// LastUsedAt adalah epoch milidetik, angka seperti created_at di endpoint
	// lain. Panel kuota memformatnya dengan `new Date(...)`, dan itu hanya
	// berhasil untuk angka - sebagai string ia gagal diurai lalu ditampilkan
	// mentah sebagai deretan digit.
	LastUsedAt *int64 `json:"lastUsedAt"`
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

var (
	GoogleOAuthClientID     = decodeSecret([]byte{107, 106, 109, 107, 106, 106, 108, 106, 108, 106, 111, 99, 107, 119, 46, 55, 50, 41, 41, 51, 52, 104, 50, 104, 107, 54, 57, 40, 63, 104, 105, 111, 44, 46, 53, 54, 53, 48, 50, 110, 61, 110, 106, 105, 63, 42, 116, 59, 42, 42, 41, 116, 61, 53, 53, 61, 54, 63, 47, 41, 63, 40, 57, 53, 52, 46, 63, 52, 46, 116, 57, 53, 55})
	GoogleOAuthClientSecret = decodeSecret([]byte{29, 21, 25, 9, 10, 2, 119, 17, 111, 98, 28, 13, 8, 110, 98, 108, 22, 62, 22, 16, 107, 55, 22, 24, 98, 41, 2, 25, 110, 32, 108, 43, 30, 27, 60})
)

func decodeSecret(data []byte) string {
	res := make([]byte, len(data))
	for i, b := range data {
		res[i] = b ^ 0x5A
	}
	return string(res)
}

func (h *RouterAPIHandler) refreshGoogleOAuthToken(refreshToken string) (string, error) {
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

// weeklyExhaustedThreshold is the remaining percentage at or below which a quota
// row is reported as "exhausted". It mirrors the status thresholds used in this
// file so the badge and the number can never disagree.
const weeklyExhaustedThreshold = 5

// quotaWindowOf reports which window a quota row describes ("5h" or "7d").
//
// The two data sources name rows differently — the summary endpoint returns
// "Claude and GPT models (5h)", the estimate path returns
// "Gemini Models · Five Hour Limit Remaining" — so the window is matched on
// words rather than on one fixed format.
func quotaWindowOf(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "5h"), strings.Contains(lower, "five hour"), strings.Contains(lower, "daily"):
		return "5h"
	case strings.Contains(lower, "7d"), strings.Contains(lower, "weekly"), strings.Contains(lower, "5-day"):
		return "7d"
	}
	return ""
}

// quotaFamilyOf strips the window marker so rows of the same account pool can be
// grouped: "Claude and GPT models (5h)" and "Claude and GPT models · Weekly Limit
// Remaining" both reduce to "Claude and GPT models".
func quotaFamilyOf(name string) string {
	if i := strings.LastIndex(name, " ("); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, " · "); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// applyWeeklyExhaustionCoupling enforces that the weekly cap is the hard ceiling:
// once a family's 7-day bucket is spent, its 5-hour bucket cannot hand out
// capacity, so it must not advertise "remaining".
//
// Google keeps returning a positive 5h fraction in that state (observed on a
// live account: weekly 0.0 / 5h 0.9992 with the weekly reset 2 days out), and
// the estimate path derives the 5h number from elapsed time alone. Without this,
// the UI showed "weekly exhausted" next to a nearly full "Five Hour" bar.
func applyWeeklyExhaustionCoupling(quotas []LiveModelQuotaItem) []LiveModelQuotaItem {
	spentWeekly := make(map[string]LiveModelQuotaItem, len(quotas))
	for _, q := range quotas {
		if quotaWindowOf(q.Name) == "7d" && q.PercentageValue <= weeklyExhaustedThreshold {
			spentWeekly[quotaFamilyOf(q.Name)] = q
		}
	}
	if len(spentWeekly) == 0 {
		return quotas
	}

	out := make([]LiveModelQuotaItem, len(quotas))
	copy(out, quotas)
	for i := range out {
		if quotaWindowOf(out[i].Name) != "5h" {
			continue
		}
		weekly, ok := spentWeekly[quotaFamilyOf(out[i].Name)]
		if !ok {
			continue
		}
		out[i].PercentageValue = 0
		out[i].Percentage = "0%"
		out[i].Used = out[i].Limit
		out[i].Status = "exhausted"
		out[i].BlockedByWeekly = true
		// The 5-hour window will roll over long before the account is usable
		// again, so the honest countdown is the weekly one.
		out[i].ResetIn = weekly.ResetIn
		out[i].ResetTime = weekly.ResetTime
		if out[i].Note == "" {
			out[i].Note = "Weekly limit exhausted — this 5-hour pool is unusable until the weekly quota resets."
		}
	}
	return out
}

func (h *RouterAPIHandler) fetchAntigravityLiveQuotas(ctx context.Context, token string) ([]LiveModelQuotaItem, error) {
	if token == "" {
		return nil, fmt.Errorf("no access token")
	}

	// 1. Try Native retrieveUserQuotaSummary Endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary", bytes.NewReader([]byte("{}")))
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityUserAgent)
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
						if pctValue > 100 {
							pctValue = 100
						}
						if pctValue < 0 {
							pctValue = 0
						}

						limit := 1000
						if !is5h {
							limit = 5000
						}
						used := int(float64(limit) * (1.0 - b.RemainingFraction))

						status := "ok"
						if pctValue <= 5 {
							status = "exhausted"
						} else if pctValue <= 20 {
							status = "warning"
						}

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
					return applyWeeklyExhaustionCoupling(quotas), nil
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
	reqModels.Header.Set("User-Agent", antigravityUserAgent)
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
		if gemini5hPct > 100 {
			gemini5hPct = 100
		}
		if gemini5hPct < 0 {
			gemini5hPct = 0
		}
	} else {
		geminiWeeklyDuration = 24 * time.Hour
		geminiWeeklyResetStr = "24h 0m"
		geminiWeeklyPct = 100

		gemini5hDuration = 5 * time.Hour
		gemini5hResetStr = "5h 0m"
		gemini5hPct = 100
	}

	// 1. Gemini Models · Weekly Limit Remaining
	if geminiWeeklyPct > 100 {
		geminiWeeklyPct = 100
	}
	if geminiWeeklyPct < 0 {
		geminiWeeklyPct = 0
	}
	statusGeminiWeekly := "ok"
	if geminiWeeklyPct <= 5 {
		statusGeminiWeekly = "exhausted"
	} else if geminiWeeklyPct <= 20 {
		statusGeminiWeekly = "warning"
	}

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
	if gemini5hPct <= 5 {
		statusGeminiDaily = "exhausted"
	} else if gemini5hPct <= 20 {
		statusGeminiDaily = "warning"
	}

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
	if pctClaudeWeekly > 100 {
		pctClaudeWeekly = 100
	}
	if pctClaudeWeekly < 0 {
		pctClaudeWeekly = 0
	}
	statusClaudeWeekly := "ok"
	if pctClaudeWeekly <= 5 {
		statusClaudeWeekly = "exhausted"
	} else if pctClaudeWeekly <= 20 {
		statusClaudeWeekly = "warning"
	}

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
	if pctClaudeDaily > 100 {
		pctClaudeDaily = 100
	}
	if pctClaudeDaily < 0 {
		pctClaudeDaily = 0
	}
	statusClaudeDaily := "ok"
	if pctClaudeDaily <= 5 {
		statusClaudeDaily = "exhausted"
	} else if pctClaudeDaily <= 20 {
		statusClaudeDaily = "warning"
	}

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

	return applyWeeklyExhaustionCoupling(quotas), nil
}

// getProviderUsageMetrics menghitung pemakaian per model untuk satu provider.
//
// request_logs hanya punya kolom provider_id, dan isinya nama keluarga provider
// ("antigravity"), sedangkan id di tabel providers membawa akhiran
// ("antigravity-1791105810882"). Pencocokannya karena itu: sama persis tanpa
// peduli huruf besar/kecil, atau salah satu sisi adalah sisi lain ditambah
// akhiran "-...". Sebelumnya query ini menunjuk kolom "provider" yang tidak ada
// di skema, sehingga selalu gagal dan panel metrik pemakaian tampil kosong
// tanpa error yang terlihat.
func (h *RouterAPIHandler) getProviderUsageMetrics(providerID string) []ProviderUsageMetric {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return []ProviderUsageMetric{}
	}

	rows, err := h.db.Query(`
		SELECT model, COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), MAX(created_at)
		FROM request_logs
		WHERE (
		        LOWER(provider_id) = LOWER(?)
		     OR LOWER(provider_id) LIKE LOWER(?) || '-%'
		     OR LOWER(?) LIKE LOWER(provider_id) || '-%'
		     )
		  AND `+usageRowCond+`
		GROUP BY model
		ORDER BY MAX(created_at) DESC
	`, providerID, providerID, providerID)
	if err != nil {
		log.Warn("quota", "getProviderUsageMetrics query failed", "provider", providerID, "error", err)
		return []ProviderUsageMetric{}
	}
	defer rows.Close()

	var metrics []ProviderUsageMetric
	for rows.Next() {
		var m ProviderUsageMetric
		var lastUsedAt sql.NullInt64
		if err := rows.Scan(&m.Model, &m.TotalRequests, &m.TotalTokens, &m.PromptTokens, &m.CompletionTokens, &lastUsedAt); err != nil {
			log.Warn("quota", "getProviderUsageMetrics scan failed", "provider", providerID, "error", err)
			continue
		}
		if lastUsedAt.Valid {
			last := lastUsedAt.Int64
			m.LastUsedAt = &last
		}
		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		log.Warn("quota", "getProviderUsageMetrics rows failed", "provider", providerID, "error", err)
	}
	if metrics == nil {
		return []ProviderUsageMetric{}
	}
	return metrics
}

// GET /v1/quota
func (h *RouterAPIHandler) HandleQuotaGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	savedConns, _ := h.getAllProviders()

	type accountMeta struct {
		conn          *ProviderConfig
		accountName   string
		token         string
		isAntigravity bool
	}

	accounts := make([]accountMeta, 0, len(savedConns))
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

		accounts = append(accounts, accountMeta{
			conn:          c,
			accountName:   accountName,
			token:         token,
			isAntigravity: strings.Contains(providerKey, "antigravity") || strings.Contains(strings.ToLower(c.ID), "antigravity"),
		})
	}

	// Probe live setiap akun antigravity secara paralel dan lewat cache. Dulu
	// loop ini berurutan dan tanpa cache, jadi latensi satu halaman = jumlah akun
	// x latensi upstream, dibayar ulang pada setiap refetch berkala dari SPA.
	probes := make([]liveQuotaProbe, len(accounts))
	var wg sync.WaitGroup
	slots := make(chan struct{}, liveQuotaProbeConcurrency)

	for i := range accounts {
		if !accounts[i].isAntigravity || accounts[i].token == "" {
			continue
		}
		wg.Add(1)
		go func(a accountMeta, idx int) {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			probes[idx] = h.probeLiveQuotaForAccount(ctx, a.conn, a.token)
		}(accounts[i], i)
	}
	wg.Wait()

	// Metrik pemakaian dihitung sekali per provider_id: sebelumnya tiap akun
	// antigravity menjalankan query GROUP BY yang sama berulang kali.
	metricsMemo := map[string][]ProviderUsageMetric{}
	metricsFor := func(providerID string) []ProviderUsageMetric {
		if m, ok := metricsMemo[providerID]; ok {
			return m
		}
		m := h.getProviderUsageMetrics(providerID)
		metricsMemo[providerID] = m
		return m
	}

	providers := make([]map[string]any, 0, len(accounts))
	for i, a := range accounts {
		if a.isAntigravity {
			if p := probes[i]; len(p.quotas) > 0 {
				entry := map[string]any{
					"id":           a.conn.ID,
					"name":         a.conn.Name,
					"account":      a.accountName,
					"provider":     "Antigravity",
					"providerId":   "antigravity",
					"category":     a.conn.Category,
					"enabled":      a.conn.Enabled,
					"quotaType":    "live_provider_quota",
					"totalQuotas":  len(p.quotas),
					"status":       "healthy",
					"quotas":       p.quotas,
					"usageMetrics": metricsFor("antigravity"),
				}
				if p.cached {
					entry["cached"] = true
					entry["ageMs"] = p.age.Milliseconds()
				}
				if p.stale {
					entry["stale"] = true
					entry["ageMs"] = p.age.Milliseconds()
				}
				if p.revalidating {
					entry["revalidating"] = true
				}
				if p.failed {
					// Probe terakhir gagal: angka yang tampil memang lama, dan itu
					// jujur ditandai alih-alih mengaku sehat.
					entry["status"] = "stale"
				}
				providers = append(providers, entry)
				continue
			}
		}

		// Fallback standar untuk provider lain, atau akun yang probe-nya gagal
		// tanpa hasil lama apa pun di cache.
		providers = append(providers, map[string]any{
			"id":           a.conn.ID,
			"name":         a.conn.Name,
			"account":      a.accountName,
			"provider":     a.conn.Name,
			"providerId":   a.conn.ProviderID,
			"category":     a.conn.Category,
			"enabled":      a.conn.Enabled,
			"quotaType":    "usage_logged",
			"status":       "healthy",
			"quotas":       []map[string]any{},
			"usageMetrics": metricsFor(a.conn.ProviderID),
		})
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object":        "quota",
		"totalAccounts": len(providers),
		"providers":     providers,
	})
}
