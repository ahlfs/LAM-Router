package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// CachedResponse stores completed chat responses
type CachedResponse struct {
	ResponseBytes []byte
	ContentType   string
	IsStream      bool
	Chunks        [][]byte
	ExpiresAt     time.Time
	CreatedAt     time.Time
	Model         string
	TokensSaved   int
}

// MemoryCache manages in-memory exact response caching with TTL
type MemoryCache struct {
	mu      sync.RWMutex
	items   map[string]*CachedResponse
	enabled bool
	ttl     time.Duration
	hits    int64
	misses  int64
	saved   int64
}

var globalCache = &MemoryCache{
	items:   make(map[string]*CachedResponse),
	enabled: true,
	ttl:     1 * time.Hour, // Default 1 hour TTL
}

// GetGlobalCache returns the singleton cache instance
func GetGlobalCache() *MemoryCache {
	return globalCache
}

// SetEnabled toggles caching on/off
func (c *MemoryCache) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

// IsEnabled returns current caching status
func (c *MemoryCache) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// SetTTL configures TTL duration
func (c *MemoryCache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
}

// GenerateKey computes a deterministic SHA-256 hash for the request
func (c *MemoryCache) GenerateKey(model string, requestBody []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		return "", err
	}

	// Filter and normalize key parameters that determine idempotency
	normalized := map[string]any{
		"model":       model,
		"messages":    payload["messages"],
		"tools":       payload["tools"],
		"temperature": payload["temperature"],
		"top_p":       payload["top_p"],
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

// Get retrieves a cached response if valid and not expired
func (c *MemoryCache) Get(key string) (*CachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return nil, false
	}

	item, exists := c.items[key]
	if !exists {
		c.misses++
		return nil, false
	}

	if time.Now().After(item.ExpiresAt) {
		delete(c.items, key)
		c.misses++
		return nil, false
	}

	c.hits++
	c.saved += int64(item.TokensSaved)
	return item, true
}

// Set stores a response in cache with configured TTL
func (c *MemoryCache) Set(key string, model string, respBytes []byte, contentType string, isStream bool, chunks [][]byte, tokensSaved int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return
	}

	// Prune expired entries if cache is growing
	if len(c.items) > 5000 {
		now := time.Now()
		for k, v := range c.items {
			if now.After(v.ExpiresAt) {
				delete(c.items, k)
			}
		}
	}

	c.items[key] = &CachedResponse{
		ResponseBytes: respBytes,
		ContentType:   contentType,
		IsStream:      isStream,
		Chunks:        chunks,
		ExpiresAt:     time.Now().Add(c.ttl),
		CreatedAt:     time.Now(),
		Model:         model,
		TokensSaved:   tokensSaved,
	}
}

// Clear flushes all cached entries
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*CachedResponse)
}

// Stats returns cache telemetry
func (c *MemoryCache) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	activeCount := 0
	totalBytes := 0
	now := time.Now()
	for _, v := range c.items {
		if now.Before(v.ExpiresAt) {
			activeCount++
			totalBytes += len(v.ResponseBytes)
			for _, chunk := range v.Chunks {
				totalBytes += len(chunk)
			}
		}
	}

	return map[string]any{
		"enabled":     c.enabled,
		"ttlSeconds":  int(c.ttl.Seconds()),
		"totalCached": activeCount,
		"totalBytes":  totalBytes,
		"hits":        c.hits,
		"misses":      c.misses,
		"tokensSaved": c.saved,
	}
}
