package routerapi

import (
	"time"
)

type ModelHealthRecord struct {
	ModelID       string `json:"model_id"`
	ProviderID    string `json:"provider_id"`
	Status        string `json:"status"` // healthy, offline, probing, untested
	LatencyMs     int64  `json:"latency_ms"`
	StatusCode    int    `json:"status_code"`
	LastError     string `json:"last_error,omitempty"`
	LastCheckedAt int64  `json:"last_checked_at"`
	IsEnabled     bool   `json:"is_enabled"`
}

func (h *RouterAPIHandler) InitHealthSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS model_health (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    status TEXT NOT NULL,
    latency_ms INTEGER DEFAULT 0,
    status_code INTEGER DEFAULT 0,
    last_error TEXT,
    last_checked_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS disabled_models (
    model_id TEXT PRIMARY KEY,
    provider_id TEXT,
    reason TEXT,
    disabled_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_models (
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    display_name TEXT,
    context_length INTEGER DEFAULT 0,
    is_custom INTEGER DEFAULT 0,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (provider_id, model_id)
);
`
	_, err := h.db.Exec(schema)
	return err
}

func (h *RouterAPIHandler) GetModelHealthRecords() ([]ModelHealthRecord, error) {
	rows, err := h.db.Query(`
		SELECT 
			h.model_id, 
			h.provider_id, 
			h.status, 
			h.latency_ms, 
			h.status_code, 
			COALESCE(h.last_error, ''), 
			h.last_checked_at,
			CASE 
				WHEN d.model_id IS NOT NULL THEN 0
				WHEN pm.is_active = 0 THEN 0
				ELSE 1
			END as is_enabled
		FROM model_health h
		LEFT JOIN disabled_models d ON LOWER(d.model_id) = LOWER(h.model_id)
		LEFT JOIN provider_models pm ON LOWER(pm.model_id) = LOWER(h.model_id)
		ORDER BY h.model_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModelHealthRecord
	for rows.Next() {
		var rec ModelHealthRecord
		var lastErr string
		var isEnabledInt int
		if err := rows.Scan(&rec.ModelID, &rec.ProviderID, &rec.Status, &rec.LatencyMs, &rec.StatusCode, &lastErr, &rec.LastCheckedAt, &isEnabledInt); err != nil {
			return nil, err
		}
		rec.LastError = lastErr
		rec.IsEnabled = (isEnabledInt == 1)
		results = append(results, rec)
	}
	return results, rows.Err()
}

func (h *RouterAPIHandler) UpsertModelHealth(rec ModelHealthRecord) error {
	query := `
INSERT INTO model_health (model_id, provider_id, status, latency_ms, status_code, last_error, last_checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(model_id) DO UPDATE SET
    provider_id = excluded.provider_id,
    status = excluded.status,
    latency_ms = excluded.latency_ms,
    status_code = excluded.status_code,
    last_error = excluded.last_error,
    last_checked_at = excluded.last_checked_at;
`
	_, err := h.db.Exec(query, rec.ModelID, rec.ProviderID, rec.Status, rec.LatencyMs, rec.StatusCode, rec.LastError, rec.LastCheckedAt)
	return err
}

func (h *RouterAPIHandler) ResetModelHealth() error {
	now := time.Now().Unix()
	_, err := h.db.Exec(`UPDATE model_health SET status = 'untested', latency_ms = 0, status_code = 0, last_error = '', last_checked_at = ?`, now)
	return err
}
