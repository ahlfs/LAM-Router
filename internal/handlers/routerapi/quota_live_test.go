package routerapi

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestLiveAntigravityQuotaGroups adalah cek live yang opt-in: ia memanggil
// endpoint Cloud Code memakai token akun yang tersimpan di DB produksi dan
// memastikan payload ringkasan kuota masih memuat grup "Gemini Models".
//
// Google membatasi isi payload berdasarkan identitas klien di header
// User-Agent: hanya identitas hub yang mutakhir yang dilayani ringkasan penuh
// (grup Gemini + grup Claude/GPT). Identitas lain dilayani payload terpotong
// yang membuang grup Gemini tanpa error apa pun, sehingga kuota Gemini hilang
// dari dashboard secara diam-diam. Tes ini menjaga regresi itu.
//
//	LAMROUTER_LIVE_QUOTA_DB=/home/ahlfs/.lam-router/db/data.sqlite \
//	  go test ./internal/handlers/routerapi/ -run TestLiveAntigravityQuota -v
func TestLiveAntigravityQuotaGroups(t *testing.T) {
	dbPath := os.Getenv("LAMROUTER_LIVE_QUOTA_DB")
	if dbPath == "" {
		t.Skip("LAMROUTER_LIVE_QUOTA_DB tidak diset: lewati cek live")
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("buka DB: %v", err)
	}
	defer dbConn.Close()

	rows, err := dbConn.Query(
		`SELECT id, COALESCE(NULLIF(access_token, ''), api_key), COALESCE(refresh_token, '')
		   FROM providers
		  WHERE provider_id LIKE '%antigravity%'
		    AND COALESCE(NULLIF(access_token, ''), api_key) IS NOT NULL`)
	if err != nil {
		t.Fatalf("query providers: %v", err)
	}
	defer rows.Close()

	type account struct{ id, token, refresh string }
	var accounts []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.id, &a.token, &a.refresh); err != nil {
			t.Fatalf("scan provider: %v", err)
		}
		accounts = append(accounts, a)
	}
	if len(accounts) == 0 {
		t.Skip("tidak ada akun antigravity di DB")
	}

	h := &RouterAPIHandler{db: dbConn}
	checked, stale := 0, 0

	for _, a := range accounts {
		quotas, err := h.fetchAntigravityLiveQuotas(context.Background(), a.token)
		if err != nil && strings.Contains(err.Error(), "HTTP 401") && a.refresh != "" {
			// Tirukan HandleQuotaGet: refresh token lalu ulangi sekali.
			if fresh, refErr := h.refreshGoogleOAuthToken(a.refresh); refErr == nil && fresh != "" {
				quotas, err = h.fetchAntigravityLiveQuotas(context.Background(), fresh)
			}
		}
		if err != nil {
			if strings.Contains(err.Error(), "HTTP 401") {
				stale++
				t.Logf("[%s] token kedaluwarsa (401) — dilewati", a.id)
				continue
			}
			t.Errorf("[%s] fetch kuota gagal: %v", a.id, err)
			continue
		}

		checked++
		var names []string
		hasGemini, hasClaude := false, false
		for _, q := range quotas {
			names = append(names, q.Name)
			if strings.Contains(q.Name, "Gemini") {
				hasGemini = true
			}
			if strings.Contains(q.Name, "Claude") {
				hasClaude = true
			}
		}
		t.Logf("[%s] %d entri kuota: %v", a.id, len(quotas), names)

		if !hasClaude {
			t.Errorf("[%s] grup Claude hilang dari payload: %v", a.id, names)
		}
		if !hasGemini {
			t.Errorf("[%s] grup Gemini HILANG dari payload ringkasan kuota: %v\n"+
				"        penyebab paling mungkin: User-Agent klien tidak lagi "+
				"dianggap sebagai identitas klien yang didukung", a.id, names)
		}
	}

	if checked == 0 {
		t.Skipf("semua %d token kedaluwarsa; tidak ada yang bisa diverifikasi", stale)
	}
}
