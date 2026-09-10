package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// extractCleanProjectID extracts a 20-char Framer project ID from full slug like 'Glamorous-Mission--ArlB75wXKSdrSqwegvuo-eLuIe'
func extractCleanProjectID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "XTFHMppmxOigqVg7Ti18"
	}
	if strings.Contains(raw, "--") {
		parts := strings.Split(raw, "--")
		if len(parts) >= 2 {
			sub := parts[1]
			if idx := strings.Index(sub, "-"); idx != -1 {
				sub = sub[:idx]
			}
			if len(sub) >= 15 {
				return sub
			}
		}
	}
	if len(raw) > 25 && strings.Contains(raw, "-") {
		parts := strings.Split(raw, "-")
		for _, p := range parts {
			if len(p) >= 18 && len(p) <= 24 {
				return p
			}
		}
	}
	return raw
}

// ForwardFramer handles wrapping incoming OpenAI chat requests into Framer's internal AI v3 chat payload format.
func ForwardFramer(w http.ResponseWriter, req *Request) error {
	var openaiReq struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
		Stream   bool            `json:"stream"`
	}

	if err := json.Unmarshal(req.Body, &openaiReq); err != nil {
		return fmt.Errorf("ForwardFramer unmarshal client body: %w", err)
	}

	// Default model to google/gemini-3-flash-preview if clean or generic
	model := openaiReq.Model
	if model == "" || strings.HasPrefix(model, "framer/") {
		model = strings.TrimPrefix(model, "framer/")
	}
	if model == "" {
		model = "google/gemini-3-flash-preview"
	}

	// Framer API requires a clean 20-char projectId field in envelope
	projectID := extractCleanProjectID(req.ProjectID)

	// Construct optional session identifier
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().Unix())
	}

	// Clean payload with required projectId
	framerPayload := map[string]interface{}{
		"projectId":       projectID,
		"featureCategory": "agents",
		"featureName":     "agents",
		"step":            "generate-session-title",
		"request": map[string]interface{}{
			"model":      model,
			"messages":   openaiReq.Messages,
			"usage":      map[string]bool{"include": true},
			"session_id": sessionID,
		},
	}

	framerBody, err := json.Marshal(framerPayload)
	if err != nil {
		return fmt.Errorf("ForwardFramer marshal: %w", err)
	}

	endpoint := "https://api.framer.com/ai/v3/chat/"
	if req.Config != nil && req.Config.BaseURL != "" {
		endpoint = req.Config.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(req.Ctx, "POST", endpoint, bytes.NewReader(framerBody))
	if err != nil {
		return fmt.Errorf("ForwardFramer create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", "https://framer.com")
	httpReq.Header.Set("Referer", "https://framer.com/")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")

	if req.APIKey != "" {
		if strings.HasPrefix(req.APIKey, "Bearer ") {
			httpReq.Header.Set("Authorization", req.APIKey)
		} else if strings.Contains(req.APIKey, "=") {
			// If cookie string is provided as key
			httpReq.Header.Set("Cookie", req.APIKey)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
		}
	}

	resp, err := req.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ForwardFramer upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ForwardFramer read upstream body: %w", err)
	}

	// If client requested stream, synthesize SSE chunk so Playground stream works seamlessly
	if req.IsStream && resp.StatusCode == 200 {
		var chatComp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBody, &chatComp); err == nil && len(chatComp.Choices) > 0 {
			content := chatComp.Choices[0].Message.Content
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			chunk := map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{
							"content": content,
						},
						"finish_reason": nil,
					},
				},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if req.ResponseBuf != nil {
				req.ResponseBuf.Write([]byte(content))
			}
			return nil
		}
	}

	// Normal Non-Streaming Response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	if req.ResponseBuf != nil {
		req.ResponseBuf.Write(respBody)
	}
	return nil
}
