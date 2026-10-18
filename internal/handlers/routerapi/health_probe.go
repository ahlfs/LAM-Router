package routerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

func (h *RouterAPIHandler) ProbeModel(ctx context.Context, modelID string) (result ModelHealthRecord) {
	providerID := "custom"
	if strings.Contains(modelID, "/") {
		providerID = strings.Split(modelID, "/")[0]
	}

	defer func() {
		if r := recover(); r != nil {
			result = ModelHealthRecord{
				ModelID:       modelID,
				ProviderID:    providerID,
				Status:        "offline",
				LatencyMs:     0,
				StatusCode:    http.StatusInternalServerError,
				LastError:     fmt.Sprintf("probe panic: %v", r),
				LastCheckedAt: time.Now().Unix(),
			}
			if h.db != nil {
				_ = h.UpsertModelHealth(result)
			}
		}
	}()

	payload := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req = req.WithContext(probeCtx)

	rec := httptest.NewRecorder()

	start := time.Now()
	if h.ChatHandler != nil {
		h.ChatHandler(rec, req)
	} else {
		rec.WriteHeader(http.StatusNotFound)
		rec.WriteString(`{"error":{"message":"no chat completion handler wired"}}`)
	}
	latency := time.Since(start).Milliseconds()

	status := "healthy"
	errMsg := ""
	if rec.Code != http.StatusOK {
		status = "offline"
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err == nil {
			if errResp.Error.Message != "" {
				errMsg = errResp.Error.Message
			} else if errResp.Message != "" {
				errMsg = errResp.Message
			}
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	result = ModelHealthRecord{
		ModelID:       modelID,
		ProviderID:    providerID,
		Status:        status,
		LatencyMs:     latency,
		StatusCode:    rec.Code,
		LastError:     errMsg,
		LastCheckedAt: time.Now().Unix(),
	}

	if h.db != nil {
		_ = h.UpsertModelHealth(result)
	}
	return result
}

func (h *RouterAPIHandler) ProbeAllModels(ctx context.Context, concurrency int) ([]ModelHealthRecord, error) {
	if concurrency <= 0 {
		concurrency = 3
	}

	allModels := h.getAllConfiguredModelIDs()
	if len(allModels) == 0 {
		return nil, nil
	}

	jobs := make(chan string, len(allModels))
	for _, m := range allModels {
		jobs <- m
	}
	close(jobs)

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]ModelHealthRecord, 0, len(allModels))

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for modelID := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					rec := h.ProbeModel(ctx, modelID)
					mu.Lock()
					results = append(results, rec)
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return results, nil
}

func (h *RouterAPIHandler) getAllConfiguredModelIDs() []string {
	if h.db == nil {
		return []string{"antigravity/claude-3-7-sonnet", "antigravity/gemini-2.5-pro"}
	}

	rows, err := h.db.Query(`SELECT DISTINCT model_id FROM provider_models WHERE is_active = 1`)
	if err == nil {
		defer rows.Close()
		var models []string
		for rows.Next() {
			var mid string
			if err := rows.Scan(&mid); err == nil && mid != "" {
				models = append(models, mid)
			}
		}
		if len(models) > 0 {
			return models
		}
	}

	return []string{"antigravity/claude-3-7-sonnet", "antigravity/gemini-2.5-pro"}
}
