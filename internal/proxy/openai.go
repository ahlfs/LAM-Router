package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"9router/proxy/internal/providers"
)

// ForwardOpenAI sends an OpenAI-format request to the provider endpoint.
func ForwardOpenAI(ctx context.Context, client *http.Client, cfg *providers.ProviderConfig, apiKey string, body []byte, isStream bool) (*http.Response, error) {
	headers := map[string]string{}
	if !cfg.NoAuth {
		switch cfg.AuthScheme {
		case "bearer":
			headers[cfg.AuthHeader] = "Bearer " + apiKey
		case "raw":
			headers[cfg.AuthHeader] = apiKey
		default:
			headers["Authorization"] = "Bearer " + apiKey
		}
	}
	for k, v := range cfg.StaticHeaders {
		headers[k] = v
	}
	if isStream {
		headers["Accept"] = "text/event-stream"
	}
	url := cfg.BaseURL
	if !strings.HasSuffix(url, "/chat/completions") && !strings.HasSuffix(url, "/messages") && !strings.HasSuffix(url, "/responses") && !strings.HasSuffix(url, "/embeddings") && !strings.HasSuffix(url, "/images/generations") {
		if strings.HasSuffix(url, "/v1") || strings.HasSuffix(url, "/v1/") {
			url = strings.TrimRight(url, "/") + "/chat/completions"
		} else if !strings.Contains(url, "/chat") {
			url = strings.TrimRight(url, "/") + "/v1/chat/completions"
		}
	}

	resp, err := DoRequest(ctx, client, "POST", url, headers, body)
	if err != nil {
		return nil, fmt.Errorf("forward to %s: %w", url, err)
	}
	return resp, nil
}

// ReadBody reads and returns the response body (capped to prevent
// unbounded memory use), closing it.
func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

// UpstreamBody reads the body and wraps non-200 as UpstreamError.
func UpstreamBody(resp *http.Response) ([]byte, error) {
	body, err := ReadBody(resp)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: body}
	}
	return body, nil
}

// BuildURL joins a base URL with a path segment.
func BuildURL(base, path string) string {
	if base == "" {
		return path
	}
	if path == "" {
		return base
	}
	return fmt.Sprintf("%s/%s", base, path)
}
