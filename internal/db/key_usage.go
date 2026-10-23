package db

import (
	"database/sql"
	"strings"
	"time"
)

// ModelUsageRow is one (API key, model) accumulator. The rollup answers "which
// models does this key actually spend tokens on" without scanning request_logs,
// which retention prunes after logRetentionDays.
type ModelUsageRow struct {
	Model       string  `json:"model"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"totalTokens"`
	Cost        float64 `json:"cost"`
	FirstUsed   int64   `json:"firstUsedAt"`
	LastUsed    int64   `json:"lastUsedAt"`
}

// KeyUsageSummary is the per-key envelope the dashboard's API Usage section
// reads: the lifetime counters already stored on api_keys plus the per-model
// breakdown. TrackedTokens is the rollup's own sum, kept separate from
// UsageTokens so a pre-existing counter drift stays visible instead of being
// silently papered over.
type KeyUsageSummary struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	QuotaLimit    int64           `json:"quotaLimit"`
	LastModel     string          `json:"lastModel"`
	LastUsedAt    int64           `json:"lastUsedAt"`
	UsageTokens   int64           `json:"usageTokens"`
	UsageCost     float64         `json:"usageCost"`
	TrackedTokens int64           `json:"trackedTokens"`
	Requests      int64           `json:"requests"`
	ModelCount    int64           `json:"modelCount"`
	Models        []ModelUsageRow `json:"models"`
}

// normalizeUsageModel drops values that are not models. Auth failures land in
// request_logs.model as "auth", and an empty model would otherwise become a
// rollup row that can never be attributed.
func normalizeUsageModel(model string) string {
	m := strings.TrimSpace(model)
	switch strings.ToLower(m) {
	case "", "auth", "none", "unknown", "n/a":
		return ""
	}
	return m
}

// IncrementApiKeyUsage books one request against a client API key: lifetime
// counters, the last-used stamp, and the per-model rollup. All three writes
// share one transaction so a failure cannot leave the counters ahead of the
// breakdown. Leaving model empty keeps the old counter-only behaviour.
func (r *Repo) IncrementApiKeyUsage(keyOrID string, tokens int, cost float64, model string) error {
	keyOrID = strings.TrimSpace(keyOrID)
	if keyOrID == "" {
		return nil
	}
	model = normalizeUsageModel(model)

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	if model != "" {
		if _, err := tx.Exec(
			`UPDATE api_keys SET usage_tokens = usage_tokens + ?, usage_cost = usage_cost + ?,
			 last_used_at = ?, last_model = ? WHERE key = ? OR id = ?`,
			tokens, cost, now, model, keyOrID, keyOrID,
		); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE api_keys SET usage_tokens = usage_tokens + ?, usage_cost = usage_cost + ? WHERE key = ? OR id = ?`,
			tokens, cost, keyOrID, keyOrID,
		); err != nil {
			return err
		}
	}

	var keyID string
	if err := tx.QueryRow(`SELECT id FROM api_keys WHERE key = ? OR id = ? LIMIT 1`, keyOrID, keyOrID).Scan(&keyID); err != nil {
		if err == sql.ErrNoRows {
			// Revoked or unknown key: nothing to attribute, keep the counters
			// untouched but do not fail the request.
			return tx.Commit()
		}
		return err
	}

	if model != "" {
		if _, err := tx.Exec(`
			INSERT INTO api_key_model_usage (key_id, model, requests, total_tokens, cost, first_used, last_used)
			VALUES (?, ?, 1, ?, ?, ?, ?)
			ON CONFLICT(key_id, model) DO UPDATE SET
				requests = requests + 1,
				total_tokens = total_tokens + excluded.total_tokens,
				cost = cost + excluded.cost,
				last_used = excluded.last_used`,
			keyID, model, tokens, cost, now, now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// BackfillKeyModelUsage seeds the rollup from request_logs once. It is a no-op
// when the rollup already has rows, so it is safe to call on every startup and
// can never double-count. Attribution is limited to rows whose api_key_id is a
// real api_keys.id, which excludes admin sessions and provider credentials that
// also appear in that column.
func (r *Repo) BackfillKeyModelUsage() (int64, error) {
	var existing int64
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM api_key_model_usage`).Scan(&existing); err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, nil
	}

	res, err := r.db.Exec(`
		INSERT INTO api_key_model_usage (key_id, model, requests, total_tokens, cost, first_used, last_used)
		SELECT api_key_id, TRIM(model), COUNT(*), COALESCE(SUM(total_tokens), 0),
		       COALESCE(SUM(estimated_cost), 0), MIN(created_at), MAX(created_at)
		FROM request_logs
		WHERE api_key_id IN (SELECT id FROM api_keys)
		  AND status_code < 400
		  AND LOWER(TRIM(COALESCE(model, ''))) NOT IN ('', 'auth', 'none', 'unknown', 'n/a')
		GROUP BY api_key_id, TRIM(model)`)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()

	// Seed the denormalized last-used fields so the section is not empty on the
	// first render after upgrading.
	_, _ = r.db.Exec(`
		UPDATE api_keys SET
			last_used_at = COALESCE((SELECT MAX(u.last_used) FROM api_key_model_usage u WHERE u.key_id = api_keys.id), 0),
			last_model   = COALESCE((SELECT u.model FROM api_key_model_usage u WHERE u.key_id = api_keys.id ORDER BY u.last_used DESC LIMIT 1), '')
		WHERE EXISTS (SELECT 1 FROM api_key_model_usage u WHERE u.key_id = api_keys.id)`)
	return affected, nil
}

// ResetKeyModelUsage clears the per-model breakdown for one key. Called by the
// explicit resetUsage path so the breakdown cannot outlive a counter reset.
func (r *Repo) ResetKeyModelUsage(keyID string) error {
	_, err := r.db.Exec(`DELETE FROM api_key_model_usage WHERE key_id = ?`, keyID)
	return err
}

// UsageRecountResult reports what a recount changed for one key. Operators ask
// for this when the lifetime counters and the per-model breakdown disagree
// (counters that predate the rollup, or a key that was edited outside the UI).
type UsageRecountResult struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	TokensBefore int64   `json:"tokensBefore"`
	TokensAfter  int64   `json:"tokensAfter"`
	CostBefore   float64 `json:"costBefore"`
	CostAfter    float64 `json:"costAfter"`
	Models       int64   `json:"models"`
}

// RecountApiKeyUsage rebuilds the lifetime counters (and the last-used stamp)
// from the per-model rollup. Passing an empty keyID recounts every key. This is
// an explicit operator action: nothing calls it automatically, because a
// deliberate reset must not be undone behind the operator's back.
func (r *Repo) RecountApiKeyUsage(keyID string) ([]UsageRecountResult, error) {
	where, args := "", []any{}
	if strings.TrimSpace(keyID) != "" {
		where, args = "WHERE id = ?", []any{strings.TrimSpace(keyID)}
	}
	rows, err := r.db.Query(`SELECT id, name, COALESCE(usage_tokens,0), COALESCE(usage_cost,0) FROM api_keys `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	type keyRow struct {
		id, name string
		tokens   int64
		cost     float64
	}
	keys := make([]keyRow, 0, 8)
	for rows.Next() {
		var k keyRow
		if err := rows.Scan(&k.id, &k.name, &k.tokens, &k.cost); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	out := make([]UsageRecountResult, 0, len(keys))
	for _, k := range keys {
		var tokens int64
		var cost float64
		var models int64
		if err := tx.QueryRow(
			`SELECT COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost),0), COUNT(*)
			 FROM api_key_model_usage WHERE key_id = ?`, k.id,
		).Scan(&tokens, &cost, &models); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE api_keys SET usage_tokens = ?, usage_cost = ?,
			last_used_at = COALESCE((SELECT MAX(last_used) FROM api_key_model_usage WHERE key_id = ?), 0),
			last_model   = COALESCE((SELECT model FROM api_key_model_usage WHERE key_id = ? ORDER BY last_used DESC LIMIT 1), '')
			WHERE id = ?`, tokens, cost, k.id, k.id, k.id); err != nil {
			return nil, err
		}
		out = append(out, UsageRecountResult{
			ID: k.id, Name: k.name,
			TokensBefore: k.tokens, TokensAfter: tokens,
			CostBefore: k.cost, CostAfter: cost,
			Models: models,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// KeyUsageSummary returns every key with its last-used stamp and the top
// modelLimit models by tokens. Reads are O(keys + modelLimit*keys): the rollup
// is bounded by distinct models per key, never by request volume.
func (r *Repo) KeyUsageSummary(modelLimit int) ([]KeyUsageSummary, error) {
	if modelLimit <= 0 {
		modelLimit = 8
	}
	rows, err := r.db.Query(`
		SELECT ak.id, ak.name, COALESCE(ak.enabled, 1), COALESCE(ak.quota_limit, 0),
		       COALESCE(ak.last_model, ''), COALESCE(ak.last_used_at, 0),
		       COALESCE(ak.usage_tokens, 0), COALESCE(ak.usage_cost, 0),
		       COALESCE((SELECT SUM(u.total_tokens) FROM api_key_model_usage u WHERE u.key_id = ak.id), 0),
		       COALESCE((SELECT SUM(u.requests) FROM api_key_model_usage u WHERE u.key_id = ak.id), 0),
		       COALESCE((SELECT COUNT(*) FROM api_key_model_usage u WHERE u.key_id = ak.id), 0)
		FROM api_keys ak ORDER BY ak.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]KeyUsageSummary, 0, 8)
	index := make(map[string]int, 8)
	for rows.Next() {
		var s KeyUsageSummary
		var enabled int
		if err := rows.Scan(&s.ID, &s.Name, &enabled, &s.QuotaLimit, &s.LastModel,
			&s.LastUsedAt, &s.UsageTokens, &s.UsageCost, &s.TrackedTokens,
			&s.Requests, &s.ModelCount); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		s.Models = []ModelUsageRow{}
		index[s.ID] = len(out)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	mrows, err := r.db.Query(`
		SELECT key_id, model, requests, total_tokens, cost, first_used, last_used FROM (
			SELECT key_id, model, requests, total_tokens, cost, first_used, last_used,
			       ROW_NUMBER() OVER (PARTITION BY key_id ORDER BY total_tokens DESC) AS rn
			FROM api_key_model_usage
		) WHERE rn <= ?`, modelLimit)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var keyID string
		var m ModelUsageRow
		if err := mrows.Scan(&keyID, &m.Model, &m.Requests, &m.TotalTokens, &m.Cost, &m.FirstUsed, &m.LastUsed); err != nil {
			return nil, err
		}
		if i, ok := index[keyID]; ok {
			out[i].Models = append(out[i].Models, m)
		}
	}
	return out, mrows.Err()
}
