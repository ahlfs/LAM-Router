package srouterapi

import (
	"encoding/json"
	"net/http"
	"time"

	"9router/proxy/internal/cache"
	"9router/proxy/internal/handlerutil"
)

// HandleCacheGet returns current exact response cache status and telemetry
func (h *SRouterHandler) HandleCacheGet(w http.ResponseWriter, r *http.Request) {
	c := cache.GetGlobalCache()
	stats := c.Stats()

	// Query persistent efficiency telemetry from SQLite request_logs
	var totalUpstreamCached, totalCompressionSaved int64
	if h.db != nil {
		_ = h.db.QueryRow(`
SELECT 
    COALESCE(SUM(cached_tokens), 0),
    COALESCE(SUM(compression_tokens), 0)
FROM request_logs
`).Scan(&totalUpstreamCached, &totalCompressionSaved)
	}

	inMemorySaved, _ := stats["tokensSaved"].(int64)
	totalSavedTokens := totalUpstreamCached + totalCompressionSaved + inMemorySaved

	// Enrich response with hybrid telemetry fields while maintaining backward compatibility
	stats["totalUpstreamCached"] = totalUpstreamCached
	stats["totalCompressionSaved"] = totalCompressionSaved
	stats["totalSavedTokens"] = totalSavedTokens

	handlerutil.WriteJSON(w, http.StatusOK, stats)
}

// HandleCacheUpdate toggles cache and updates TTL
func (h *SRouterHandler) HandleCacheUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled    *bool `json:"enabled"`
		TTLSeconds *int  `json:"ttlSeconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	c := cache.GetGlobalCache()
	if body.Enabled != nil {
		c.SetEnabled(*body.Enabled)
	}
	if body.TTLSeconds != nil && *body.TTLSeconds > 0 {
		c.SetTTL(time.Duration(*body.TTLSeconds) * time.Second)
	}

	handlerutil.WriteJSON(w, http.StatusOK, c.Stats())
}

// HandleCacheClear flushes all cached responses
func (h *SRouterHandler) HandleCacheClear(w http.ResponseWriter, r *http.Request) {
	c := cache.GetGlobalCache()
	c.Clear()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Response cache cleared successfully",
	})
}
