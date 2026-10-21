package routerapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Halaman Quotas & Limits dulu menunggu probe live yang berjalan BERURUTAN untuk
// setiap akun antigravity, tanpa cache sama sekali: latensi satu halaman = jumlah
// akun x latensi upstream, dan SPA yang me-refetch berkala membayar ulang biaya
// itu terus-menerus. Tes di berkas ini mengunci janji perbaikannya: probe paralel,
// muat berikutnya disajikan dari cache, penyegaran latar belakang (stale-while-
// revalidate) supaya tombol "Refresh live quota" tetap mendapat data baru, kuota
// lama tetap disajikan saat probe gagal, dan refresh token hanya untuk 401.

func quotaTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("buka sqlite in-memory: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := InitSchema(dbConn); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	return dbConn
}

func insertQuotaTestConn(t *testing.T, dbConn *sql.DB, id, name, accessToken, refreshToken string) {
	t.Helper()

	var rt any
	if refreshToken != "" {
		rt = refreshToken
	}
	if _, err := dbConn.Exec(`
INSERT INTO providers (id, provider_id, name, category, protocol, access_token, refresh_token, priority, enabled, created_at)
VALUES (?, 'antigravity', ?, 'oauth', 'antigravity', ?, ?, 10, 1, ?)`,
		id, name, accessToken, rt, time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert provider %s: %v", id, err)
	}
}

// quotaTestEnv menyiapkan handler dengan cache bersih dan probe jaringan yang
// dapat dikendalikan tes.
func quotaTestEnv(t *testing.T, probe func(ctx context.Context, token string) ([]LiveModelQuotaItem, error)) (*RouterAPIHandler, *int32, *sql.DB) {
	t.Helper()

	dbConn := quotaTestDB(t)
	liveQuotaCache.reset()

	prevProbe := antigravityQuotaProbe
	var probeCalls int32
	antigravityQuotaProbe = func(_ *RouterAPIHandler, ctx context.Context, token string) ([]LiveModelQuotaItem, error) {
		atomic.AddInt32(&probeCalls, 1)
		return probe(ctx, token)
	}
	t.Cleanup(func() {
		antigravityQuotaProbe = prevProbe
		liveQuotaCache.reset()
	})

	return &RouterAPIHandler{db: dbConn}, &probeCalls, dbConn
}

// withQuotaCacheWindows mengecilkan jendela cache supaya tes tidak perlu menunggu
// puluhan detik.
func withQuotaCacheWindows(t *testing.T, fresh, stale time.Duration) {
	t.Helper()

	prevFresh, prevStale := liveQuotaCacheTTL, liveQuotaStaleWindow
	liveQuotaCacheTTL, liveQuotaStaleWindow = fresh, stale
	t.Cleanup(func() { liveQuotaCacheTTL, liveQuotaStaleWindow = prevFresh, prevStale })
}

func quotaTestRequest(t *testing.T, h *RouterAPIHandler) []map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	h.HandleQuotaGet(rec, httptest.NewRequest("GET", "/v1/quota", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("urai respons: %v (body: %s)", err, rec.Body.String())
	}
	return body.Providers
}

func quotaProviderByID(t *testing.T, providers []map[string]any, id string) map[string]any {
	t.Helper()

	for _, p := range providers {
		if p["id"] == id {
			return p
		}
	}
	t.Fatalf("akun %q tidak ada di respons: %+v", id, providers)
	return nil
}

func sampleQuotaItems(tag string) []LiveModelQuotaItem {
	return []LiveModelQuotaItem{
		{Name: "Gemini Models · Five Hour Limit Remaining " + tag, Used: 100, Limit: 500000, Percentage: "80%", PercentageValue: 80, Status: "ok"},
		{Name: "Claude and GPT models · Weekly Limit Remaining " + tag, Used: 200, Limit: 2500000, Percentage: "60%", PercentageValue: 60, Status: "ok"},
	}
}

func TestQuotaGetProbesAccountsInParallel(t *testing.T) {
	h, probeCalls, dbConn := quotaTestEnv(t, func(_ context.Context, token string) ([]LiveModelQuotaItem, error) {
		time.Sleep(250 * time.Millisecond)
		return sampleQuotaItems(token), nil
	})
	insertQuotaTestConn(t, dbConn, "ag-1", "Akun Satu", "tok-1", "")
	insertQuotaTestConn(t, dbConn, "ag-2", "Akun Dua", "tok-2", "")
	insertQuotaTestConn(t, dbConn, "ag-3", "Akun Tiga", "tok-3", "")

	start := time.Now()
	providers := quotaTestRequest(t, h)
	elapsed := time.Since(start)

	if got := atomic.LoadInt32(probeCalls); got != 3 {
		t.Fatalf("probe dipanggil %d kali, mau 3", got)
	}
	// Berurutan berarti >= 750ms; paralel harus jelas di bawah itu.
	if elapsed > 600*time.Millisecond {
		t.Errorf("tiga akun diprobe berurutan (%v); harapannya paralel", elapsed)
	}

	for _, id := range []string{"ag-1", "ag-2", "ag-3"} {
		p := quotaProviderByID(t, providers, id)
		if p["quotaType"] != "live_provider_quota" {
			t.Errorf("%s quotaType = %v, mau live_provider_quota", id, p["quotaType"])
		}
		if got, _ := p["totalQuotas"].(float64); got != 2 {
			t.Errorf("%s totalQuotas = %v, mau 2", id, p["totalQuotas"])
		}
	}
}

func TestQuotaGetServesCachedProbeWithinTTL(t *testing.T) {
	h, probeCalls, dbConn := quotaTestEnv(t, func(_ context.Context, token string) ([]LiveModelQuotaItem, error) {
		return sampleQuotaItems(token), nil
	})
	insertQuotaTestConn(t, dbConn, "ag-1", "Akun Satu", "tok-1", "")

	first := quotaProviderByID(t, quotaTestRequest(t, h), "ag-1")
	if first["cached"] != nil {
		t.Errorf("muat pertama harus hasil probe segar, bukan cache: %+v", first)
	}

	second := quotaProviderByID(t, quotaTestRequest(t, h), "ag-1")
	if got := atomic.LoadInt32(probeCalls); got != 1 {
		t.Fatalf("probe dipanggil %d kali untuk dua muat halaman, mau 1", got)
	}
	if second["cached"] != true {
		t.Errorf("muat kedua tidak ditandai memakai cache: %+v", second)
	}
	if second["stale"] != nil {
		t.Errorf("hasil yang masih segar jangan ditandai stale: %+v", second)
	}
	if second["totalQuotas"] != first["totalQuotas"] {
		t.Errorf("totalQuotas dari cache = %v, mau %v", second["totalQuotas"], first["totalQuotas"])
	}
}

func TestQuotaGetServesStaleInstantlyAndRevalidatesInBackground(t *testing.T) {
	withQuotaCacheWindows(t, 15*time.Millisecond, 5*time.Second)

	probed := make(chan string, 8)
	h, probeCalls, dbConn := quotaTestEnv(t, func(_ context.Context, token string) ([]LiveModelQuotaItem, error) {
		select {
		case probed <- token:
		default:
		}
		return sampleQuotaItems(token), nil
	})
	insertQuotaTestConn(t, dbConn, "ag-1", "Akun Satu", "tok-1", "")

	quotaTestRequest(t, h)
	select {
	case <-probed:
	case <-time.After(2 * time.Second):
		t.Fatal("muat pertama tidak memanggil upstream")
	}

	// Lewati jendela segar, tapi masih di dalam jendela stale.
	time.Sleep(40 * time.Millisecond)

	start := time.Now()
	second := quotaProviderByID(t, quotaTestRequest(t, h), "ag-1")
	elapsed := time.Since(start)
	if elapsed > 60*time.Millisecond {
		t.Errorf("muat kedua menunggu upstream (%v); harapannya disajikan dari cache lalu disegarkan di latar belakang", elapsed)
	}
	if second["stale"] != true || second["revalidating"] != true {
		t.Errorf("muat kedua tidak ditandai stale+revalidating: %+v", second)
	}
	if got, _ := second["totalQuotas"].(float64); got != 2 {
		t.Errorf("muat kedua totalQuotas = %v, mau 2 (dari cache)", second["totalQuotas"])
	}

	// Penyegaran latar belakang wajib benar-benar memanggil upstream, supaya
	// tombol "Refresh live quota" dan refetch berikutnya mendapat angka baru.
	select {
	case <-probed:
	case <-time.After(2 * time.Second):
		t.Fatal("penyegaran latar belakang tidak pernah memanggil upstream")
	}
	if got := atomic.LoadInt32(probeCalls); got < 2 {
		t.Errorf("probe dipanggil %d kali, mau >= 2 (muat pertama + penyegaran)", got)
	}
}

func TestQuotaGetFallsBackToStaleQuotaWhenProbeFails(t *testing.T) {
	withQuotaCacheWindows(t, 10*time.Millisecond, 10*time.Millisecond)

	var fail atomic.Bool
	h, _, dbConn := quotaTestEnv(t, func(_ context.Context, _ string) ([]LiveModelQuotaItem, error) {
		if fail.Load() {
			return nil, fmt.Errorf("antigravity kuota live: HTTP 403: ditolak")
		}
		return sampleQuotaItems(""), nil
	})
	insertQuotaTestConn(t, dbConn, "ag-1", "Akun Satu", "tok-1", "")

	fresh := quotaProviderByID(t, quotaTestRequest(t, h), "ag-1")
	if got, _ := fresh["totalQuotas"].(float64); got != 2 {
		t.Fatalf("muat pertama totalQuotas = %v, mau 2", fresh["totalQuotas"])
	}

	// Cache kini di luar jendela stale: halaman menunggu probe sungguhan, dan
	// probe itu gagal — data lama masih lebih berguna daripada kartu kosong.
	time.Sleep(40 * time.Millisecond)
	fail.Store(true)

	stale := quotaProviderByID(t, quotaTestRequest(t, h), "ag-1")
	if stale["stale"] != true {
		t.Errorf("kegagalan probe tidak ditandai stale: %+v", stale)
	}
	if stale["status"] != "stale" {
		t.Errorf("status = %v, mau stale", stale["status"])
	}
	if got, _ := stale["totalQuotas"].(float64); got != 2 {
		t.Errorf("kuota lama tidak disajikan setelah probe gagal; totalQuotas = %v", stale["totalQuotas"])
	}
}

func TestQuotaGetRefreshesTokenOnlyOnUnauthorized(t *testing.T) {
	prevRefresh := googleTokenRefresh
	var refreshCalls int32
	googleTokenRefresh = func(_ *RouterAPIHandler, refreshToken string) (string, error) {
		atomic.AddInt32(&refreshCalls, 1)
		if refreshToken != "rt-1" {
			return "", fmt.Errorf("refresh token tak terduga: %q", refreshToken)
		}
		return "tok-baru", nil
	}
	t.Cleanup(func() { googleTokenRefresh = prevRefresh })

	h, _, dbConn := quotaTestEnv(t, func(_ context.Context, token string) ([]LiveModelQuotaItem, error) {
		switch token {
		case "tok-401":
			return nil, fmt.Errorf("antigravity kuota live: HTTP 401: token kedaluwarsa")
		case "tok-403":
			return nil, fmt.Errorf("antigravity kuota live: HTTP 403: ditolak")
		}
		return sampleQuotaItems(token), nil
	})
	insertQuotaTestConn(t, dbConn, "ag-401", "Akun 401", "tok-401", "rt-1")
	insertQuotaTestConn(t, dbConn, "ag-403", "Akun 403", "tok-403", "rt-1")

	providers := quotaTestRequest(t, h)

	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Errorf("refresh dipanggil %d kali, mau 1 (hanya untuk 401)", got)
	}

	p401 := quotaProviderByID(t, providers, "ag-401")
	if got, _ := p401["totalQuotas"].(float64); got != 2 {
		t.Errorf("akun 401 setelah refresh totalQuotas = %v, mau 2", p401["totalQuotas"])
	}
	var storedToken string
	if err := dbConn.QueryRow("SELECT access_token FROM providers WHERE id = 'ag-401'").Scan(&storedToken); err != nil {
		t.Fatalf("baca access_token: %v", err)
	}
	if storedToken != "tok-baru" {
		t.Errorf("access_token tersimpan = %q, mau tok-baru", storedToken)
	}

	// 403 bukan kegagalan kredensial: jangan refresh, dan jangan pura-pura ada
	// kuota live — akun jatuh ke fallback pemakaian tercatat.
	p403 := quotaProviderByID(t, providers, "ag-403")
	if p403["quotaType"] != "usage_logged" {
		t.Errorf("akun 403 quotaType = %v, mau usage_logged", p403["quotaType"])
	}
}
