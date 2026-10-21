package routerapi

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// getProviderUsageMetrics dahulu menunjuk kolom "provider" yang tidak ada di
// request_logs, jadi query-nya selalu gagal dan panel metrik pemakaian per
// provider tampil kosong tanpa error yang terlihat. Tes ini mengunci
// kontraknya: hitung pemakaian per model dari provider_id, cocokkan nama
// keluarga provider maupun id kredensial yang membawa akhiran, dan jangan
// pernah laporkan permintaan yang ditolak sebagai pemakaian model.
func TestGetProviderUsageMetrics(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("buka sqlite in-memory: %v", err)
	}
	defer dbConn.Close()
	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	base := time.Now().UnixMilli()
	insert := func(id, providerID, model string, status, tokens int, created int64) {
		t.Helper()
		if _, err := dbConn.Exec(`
INSERT INTO request_logs (id, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at, cached_tokens, compression_tokens, estimated_cost, fallback_reason)
VALUES (?, 'key-1', ?, ?, ?, 0, ?, ?, 12, ?, 0, 0, 0, '')`,
			id, providerID, model, tokens, tokens, status, created); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insert("log-1", "antigravity", "gemini-3.8-flash-high", 200, 1000, base-3000)
	insert("log-2", "antigravity", "gemini-3.8-flash-high", 200, 500, base-2000)
	insert("log-3", "Antigravity", "claude-sonnet-4-6", 200, 200, base-1000)
	insert("log-4", "custom", "gpt-oss-120b-medium", 200, 77, base-500)
	// Ditolak auth middleware: bentuk lama (placeholder "auth" di kedua kolom)
	// dan bentuk baru (model kosong).
	insert("log-5", "auth", "auth", 401, 0, base-400)
	insert("log-6", "auth", "", 429, 0, base-300)

	h := &RouterAPIHandler{db: dbConn}

	got := h.getProviderUsageMetrics("antigravity")
	byModel := map[string]ProviderUsageMetric{}
	for _, m := range got {
		byModel[m.Model] = m
	}
	if len(got) != 2 {
		t.Fatalf("model untuk antigravity = %d, mau 2: %+v", len(got), got)
	}
	if m := byModel["gemini-3.8-flash-high"]; m.TotalRequests != 2 || m.TotalTokens != 1500 {
		t.Errorf("gemini-3.8-flash-high = %d permintaan / %d token, mau 2 / 1500", m.TotalRequests, m.TotalTokens)
	}
	if _, ok := byModel["claude-sonnet-4-6"]; !ok {
		t.Errorf("baris dengan provider_id berhuruf besar tidak ikut terhitung: %+v", got)
	}
	if _, ok := byModel["gpt-oss-120b-medium"]; ok {
		t.Errorf("model provider lain bocor ke antigravity: %+v", got)
	}
	// lastUsedAt harus epoch milidetik (angka), bukan string: panel kuota
	// memformatnya dengan `new Date(...)` dan hanya angka yang bisa diurai.
	if m := byModel["gemini-3.8-flash-high"]; m.LastUsedAt == nil || *m.LastUsedAt != base-2000 {
		t.Errorf("lastUsedAt = %v, mau %d (epoch ms angka)", m.LastUsedAt, base-2000)
	}

	// Id kredensial (keluarga + akhiran) harus menemukan baris keluarganya.
	if got := h.getProviderUsageMetrics("antigravity-1791105810882"); len(got) != 2 {
		t.Errorf("id kredensial -> %d model, mau 2: %+v", len(got), got)
	}
	// Provider lain tetap terpisah.
	if got := h.getProviderUsageMetrics("custom"); len(got) != 1 || got[0].Model != "gpt-oss-120b-medium" {
		t.Errorf("custom = %+v, mau hanya gpt-oss-120b-medium", got)
	}
	// Permintaan yang ditolak bukan pemakaian model, walau dicari sebagai "auth".
	if got := h.getProviderUsageMetrics("auth"); len(got) != 0 {
		t.Errorf("baris tertolak dilaporkan sebagai pemakaian: %+v", got)
	}
	// Id kosong tidak boleh mencocokkan apa pun.
	if got := h.getProviderUsageMetrics("  "); len(got) != 0 {
		t.Errorf("id kosong -> %+v, mau kosong", got)
	}
}
