package routerapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type HealthSummaryResponse struct {
	Models []ModelHealthRecord `json:"models"`
	Stats  HealthStats         `json:"stats"`
}

type HealthStats struct {
	Total        int   `json:"total"`
	Healthy      int   `json:"healthy"`
	Offline      int   `json:"offline"`
	Untested     int   `json:"untested"`
	AvgLatencyMs int64 `json:"avg_latency_ms"`
}

func (h *RouterAPIHandler) HandleModelsHealthList(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var records []ModelHealthRecord
	if h.db != nil {
		var err error
		records, err = h.GetModelHealthRecords()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if records == nil {
		records = make([]ModelHealthRecord, 0)
	}

	// Calculate stats
	stats := HealthStats{Total: len(records)}
	var sumLatency int64
	var healthyCount int64

	for _, r := range records {
		switch r.Status {
		case "healthy":
			stats.Healthy++
			sumLatency += r.LatencyMs
			healthyCount++
		case "offline":
			stats.Offline++
		default:
			stats.Untested++
		}
	}
	if healthyCount > 0 {
		stats.AvgLatencyMs = sumLatency / healthyCount
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthSummaryResponse{
		Models: records,
		Stats:  stats,
	})
}

func (h *RouterAPIHandler) HandleModelsHealthProbe(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		ModelID string `json:"model_id"`
		All     bool   `json:"all"`
	}
	_ = json.NewDecoder(req.Body).Decode(&reqBody)

	if reqBody.All {
		results, err := h.ProbeAllModels(req.Context(), 3)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = make([]ModelHealthRecord, 0)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"results": results,
		})
		return
	}

	if reqBody.ModelID == "" {
		http.Error(w, "model_id required", http.StatusBadRequest)
		return
	}

	res := h.ProbeModel(req.Context(), reqBody.ModelID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *RouterAPIHandler) HandleModelsHealthReset(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.db != nil {
		if err := h.ResetModelHealth(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// POST /v1/health/models/disable-offline
func (h *RouterAPIHandler) HandleModelsHealthDisableOffline(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		ModelIDs []string `json:"model_ids"`
	}
	_ = json.NewDecoder(req.Body).Decode(&reqBody)

	targetModels := reqBody.ModelIDs
	if len(targetModels) == 0 && h.db != nil {
		rows, err := h.db.Query(`SELECT model_id FROM model_health WHERE status = 'offline'`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var mid string
				if err := rows.Scan(&mid); err == nil && mid != "" {
					targetModels = append(targetModels, mid)
				}
			}
		}
	}

	if len(targetModels) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"disabled_count":  0,
			"disabled_models": []string{},
		})
		return
	}

	now := time.Now().UnixMilli()
	disabledCount := 0
	var disabledList []string

	if h.db != nil {
		tx, err := h.db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		for _, mid := range targetModels {
			providerID := "custom"
			if strings.Contains(mid, "/") {
				providerID = strings.Split(mid, "/")[0]
			}

			// 1. Mark is_active = 0 in provider_models
			_, _ = tx.Exec(`
				INSERT INTO provider_models (provider_id, model_id, is_active, created_at)
				VALUES (?, ?, 0, ?)
				ON CONFLICT(provider_id, model_id) DO UPDATE SET is_active = 0`,
				providerID, mid, now)

			// 2. Insert into disabled_models
			_, _ = tx.Exec(`
				INSERT OR REPLACE INTO disabled_models (model_id, provider_id, reason, disabled_at)
				VALUES (?, ?, 'offline_health_check', ?)`,
				mid, providerID, now)

			disabledCount++
			disabledList = append(disabledList, mid)
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"disabled_count":  disabledCount,
		"disabled_models": disabledList,
	})
}
