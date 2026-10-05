package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ServerStatus struct {
	Online  bool
	Latency time.Duration
	BaseURL string
}

type ConnectionItem struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	Protocol   string `json:"protocol"`
	APIKey     string `json:"apiKey"`
	Enabled    bool   `json:"enabled"`
	Priority   int    `json:"priority"`
}

type ProviderItem struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Category       string           `json:"category"`
	Protocol       string           `json:"protocol"`
	ConnectedCount int              `json:"connectedCount"`
	StatusState    string           `json:"statusState"`
	Connections    []ConnectionItem `json:"connections"`
}

type ModelItem struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Name       string `json:"name"`
	OwnedBy    string `json:"ownedBy"`
}

type FallbackItem struct {
	ID          string `json:"id"`
	SourceModel string `json:"sourceModel"`
	TargetModel string `json:"targetModel"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

type APIKeyItem struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Enabled     bool    `json:"enabled"`
	UsageTokens int64   `json:"usageTokens"`
	UsageCost   float64 `json:"usageCost"`
	RateLimit   int     `json:"rateLimit"`
	QuotaLimit  int64   `json:"quotaLimit"`
	CreditLimit float64 `json:"creditLimit"`
}

type CacheStats struct {
	Enabled          bool  `json:"enabled"`
	Hits             int64 `json:"hits"`
	Misses           int64 `json:"misses"`
	TotalCached      int64 `json:"totalCached"`
	TotalSavedTokens int64 `json:"totalSavedTokens"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) CheckHealth() ServerStatus {
	start := time.Now()
	resp, err := c.http.Get(c.baseURL + "/health")
	if err != nil || (resp != nil && resp.StatusCode >= 400) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		// Fallback probe to /healthz
		resp2, err2 := c.http.Get(c.baseURL + "/healthz")
		if err2 != nil || (resp2 != nil && resp2.StatusCode >= 400) {
			if resp2 != nil {
				_ = resp2.Body.Close()
			}
			return ServerStatus{
				Online:  false,
				Latency: time.Since(start),
				BaseURL: c.baseURL,
			}
		}
		_ = resp2.Body.Close()
	} else if resp != nil {
		_ = resp.Body.Close()
	}

	return ServerStatus{
		Online:  true,
		Latency: time.Since(start),
		BaseURL: c.baseURL,
	}
}

type rawProvider struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Category    string           `json:"category"`
	Protocol    string           `json:"protocol"`
	Connections []ConnectionItem `json:"connections"`
	Status      any              `json:"status"`
}

func (c *Client) FetchProviders() ([]ProviderItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/providers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Data      []rawProvider `json:"data"`
		Providers []rawProvider `json:"providers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	list := data.Data
	if len(list) == 0 {
		list = data.Providers
	}

	var items []ProviderItem
	for _, p := range list {
		connCount := len(p.Connections)
		stateStr := "no_connections"

		if statusMap, ok := p.Status.(map[string]any); ok {
			if cc, ok := statusMap["connectedCount"].(float64); ok && int(cc) > connCount {
				connCount = int(cc)
			}
			if s, ok := statusMap["state"].(string); ok {
				stateStr = s
			}
		} else if s, ok := p.Status.(string); ok {
			stateStr = s
		}

		items = append(items, ProviderItem{
			ID:             p.ID,
			Name:           p.Name,
			Category:       p.Category,
			Protocol:       p.Protocol,
			ConnectedCount: connCount,
			StatusState:    stateStr,
			Connections:    p.Connections,
		})
	}
	return items, nil
}

func (c *Client) FetchProviderConnections(providerID string) ([]ConnectionItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/providers/" + providerID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Connections []ConnectionItem `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Connections, nil
}

func (c *Client) FetchModels() ([]ModelItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var items []ModelItem
	for _, m := range data.Data {
		parts := strings.Split(m.ID, "/")
		prov := ""
		name := m.ID
		if len(parts) > 1 {
			prov = parts[0]
			name = parts[1]
		}
		if !strings.HasPrefix(m.ID, "combo:") && !strings.HasPrefix(m.ID, "mock:") {
			items = append(items, ModelItem{
				ID:         m.ID,
				ProviderID: prov,
				Name:       name,
				OwnedBy:    m.OwnedBy,
			})
		}
	}
	return items, nil
}

func (c *Client) FetchFallbacks() ([]FallbackItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/settings/fallbacks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Fallbacks []FallbackItem `json:"fallbacks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Fallbacks, nil
}

func (c *Client) CreateFallback(sourceModel, targetModel string, priority int) error {
	payload := map[string]any{
		"sourceModel": sourceModel,
		"targetModel": targetModel,
		"priority":    priority,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/settings/fallbacks", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) DeleteFallback(id string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/settings/fallbacks/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) ToggleFallback(id string, enabled bool) error {
	payload := map[string]any{
		"enabled": enabled,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("PUT", c.baseURL+"/v1/settings/fallbacks/"+id, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) FetchCacheStats() (CacheStats, error) {
	var stats CacheStats
	resp, err := c.http.Get(c.baseURL + "/v1/settings/cache")
	if err != nil {
		return stats, err
	}
	defer resp.Body.Close()

	var data struct {
		Enabled          bool  `json:"enabled"`
		Hits             int64 `json:"hits"`
		Misses           int64 `json:"misses"`
		TotalCached      int64 `json:"totalCached"`
		TotalSavedTokens int64 `json:"totalSavedTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return stats, err
	}
	return CacheStats(data), nil
}

func (c *Client) SendPrompt(ctx context.Context, apiKey, model, prompt string) (string, time.Duration, error) {
	start := time.Now()
	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	b, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.http.Do(req)
	lat := time.Since(start)
	if err != nil {
		return "", lat, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", lat, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil && len(parsed.Choices) > 0 {
		return parsed.Choices[0].Message.Content, lat, nil
	}
	return string(bodyBytes), lat, nil
}

func (c *Client) FetchKeys() ([]APIKeyItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/keys")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Data []APIKeyItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Data, nil
}

func (c *Client) CreateKey(name string, rateLimit, quotaLimit int, creditLimit float64) (*APIKeyItem, error) {
	payload := map[string]any{
		"name":        name,
		"rateLimit":   rateLimit,
		"quotaLimit":  quotaLimit,
		"creditLimit": creditLimit,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/keys", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}

	var created APIKeyItem
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}
	return &created, nil
}

func (c *Client) UpdateKey(id, name string, enabled bool, rateLimit, quotaLimit int, creditLimit float64) error {
	payload := map[string]any{
		"name":        name,
		"enabled":     enabled,
		"rateLimit":   rateLimit,
		"quotaLimit":  quotaLimit,
		"creditLimit": creditLimit,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("PUT", c.baseURL+"/v1/keys/"+id, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) DeleteKey(id string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/keys/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) AddProviderConnection(providerID, name, category, apiKey string) error {
	payload := map[string]any{
		"providerId": providerID,
		"name":       name,
		"category":   category,
		"apiKey":     apiKey,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/providers", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) DeleteProviderConnection(providerID string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/providers/"+providerID, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bs))
	}
	return nil
}

func (c *Client) VerifyProvider(providerID, protocol, apiKey string) (bool, string, error) {
	payload := map[string]any{
		"provider": providerID,
		"protocol": protocol,
		"apiKey":   apiKey,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/providers/verify", "application/json", bytes.NewReader(b))
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	var res struct {
		Success bool   `json:"success"`
		Valid   bool   `json:"valid"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, "", err
	}
	return res.Valid, res.Message, nil
}

func (c *Client) StartOAuthLogin(provider string) (authURL, state string, err error) {
	resp, err := c.http.Get(c.baseURL + "/v1/auth/" + provider + "/login")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var data struct {
		AuthURL string `json:"authUrl"`
		State   string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}
	return data.AuthURL, data.State, nil
}

func (c *Client) ExchangeOAuthCallback(provider, code, state, callbackURL string) (string, error) {
	payload := map[string]any{
		"code":        code,
		"state":       state,
		"callbackUrl": callbackURL,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/auth/"+provider+"/callback", "application/json", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		Provider struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"provider"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode >= 400 {
		if res.Error != "" {
			return "", fmt.Errorf("%s", res.Error)
		}
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if res.Provider.Name != "" {
		return res.Provider.Name, nil
	}
	if res.Message != "" {
		return res.Message, nil
	}
	return "Connected", nil
}

type LogItem struct {
	ID               string  `json:"id"`
	APIKeyID         string  `json:"apiKeyId"`
	ProviderID       string  `json:"providerId"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	StatusCode       int     `json:"statusCode"`
	LatencyMs        int     `json:"latencyMs"`
	CreatedAt        int64   `json:"createdAt"`
	CachedTokens     int64   `json:"cachedTokens"`
	ReasoningTokens  int64   `json:"reasoningTokens"`
	EstimatedCost    float64 `json:"estimatedCost"`
	FallbackOccurred bool    `json:"fallbackOccurred"`
}

func (c *Client) FetchLogs() ([]LogItem, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/logs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Data []LogItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Data, nil
}

func (c *Client) ExportProviders() ([]map[string]any, error) {
	resp, err := c.http.Get(c.baseURL + "/v1/settings/backup/export")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Providers, nil
}

func (c *Client) ImportProviders(providers []map[string]any) (int, error) {
	payload := map[string]any{
		"providers": providers,
	}
	b, _ := json.Marshal(payload)
	resp, err := c.http.Post(c.baseURL+"/v1/settings/backup/import", "application/json", bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var res struct {
		ImportedProviders int    `json:"importedProviders"`
		Message           string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, res.Message)
	}
	return res.ImportedProviders, nil
}
