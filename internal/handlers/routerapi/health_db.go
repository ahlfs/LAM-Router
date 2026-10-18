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
`
	_, err := h.db.Exec(schema)
	return err
}

func (h *RouterAPIHandler) GetModelHealthRecords() ([]ModelHealthRecord, error) {
	rows, err := h.db.Query(`
		SELECT model_id, provider_id, status, latency_ms, status_code, COALESCE(last_error, ''), last_checked_at
		FROM model_health
		ORDER BY model_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModelHealthRecord
	for rows.Next() {
		var rec ModelHealthRecord
		var lastErr string
		if err := rows.Scan(&rec.ModelID, &rec.ProviderID, &rec.Status, &rec.LatencyMs, &rec.StatusCode, &lastErr, &rec.LastCheckedAt); err != nil {
			return nil, err
		}
		rec.LastError = lastErr
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
