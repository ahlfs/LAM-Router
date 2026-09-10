package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// refreshFramer fetches a fresh short-lived JWT access token using Framer session credentials.
func refreshFramer(ctx context.Context, p *Params) (*TokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://framer.com/api/user/access-token", nil)
	if err != nil {
		return nil, fmt.Errorf("framer refresh create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://framer.com")
	req.Header.Set("Referer", "https://framer.com/")

	// If refresh token or access token is provided as cookie
	cookieVal := p.RefreshToken
	if cookieVal == "" {
		cookieVal = p.AccessToken
	}

	if cookieVal != "" {
		if strings.Contains(cookieVal, "=") {
			req.Header.Set("Cookie", cookieVal)
		} else {
			req.Header.Set("Authorization", "Bearer "+cookieVal)
		}
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("framer refresh POST: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("framer refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Token     string `json:"token"`
		JWT       string `json:"jwt"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("framer parse json: %w", err)
	}

	token := data.Token
	if token == "" {
		token = data.JWT
	}
	if token == "" {
		// If returned plain string token
		token = strings.Trim(string(body), `" \r\n`)
	}

	expires := data.ExpiresIn
	if expires <= 0 {
		expires = 600 // 10 minutes default
	}

	return &TokenResult{
		AccessToken: token,
		ExpiresIn:   expires,
	}, nil
}
