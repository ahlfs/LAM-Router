package routerapi

import (
	"encoding/json"
	"net/http"
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
