package routerapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"lamrouter/internal/log"
)

// Kuota live Antigravity datang dari endpoint Cloud Code, bukan dari DB: satu
// probe per akun berarti 1-2 panggilan HTTPS ke Google. HandleQuotaGet dulu
// memanggilnya berurutan untuk setiap akun dan tanpa cache, sehingga setiap muat
// halaman /quota — dan setiap refetch berkala dari SPA — menunggu
// (jumlah akun x latensi upstream). Tiga akun antigravity saja sudah ~1,5-2 detik
// tiap muat, dan biayanya naik linier seiring bertambahnya akun.
//
// Strategi di berkas ini: stale-while-revalidate.
//
//	segar   (<= liveQuotaCacheTTL)     : sajikan dari cache, tanpa probe
//	setengah tua (<= liveQuotaStaleWindow): sajikan dari cache SEKARANG, lalu
//	                                        segarkan di latar belakang supaya
//	                                        muat berikutnya sudah segar
//	terlalu tua / kosong               : probe blocking (paralel antar akun di
//	                                        HandleQuotaGet), lalu simpan
//
// Jadi halaman hanya menunggu upstream pada muat pertama setelah lama menganggur;
// selebihnya instan, sementara datanya tetap segar dalam hitungan detik. Tombol
// "Refresh live quota" tetap mendapat data yang baru disegarkan di latar belakang.
const (
	// liveQuotaProbeTimeout membatasi satu probe akun, jauh di bawah timeout 10
	// detik di fetchAntigravityLiveQuotas supaya satu akun yang menggantung tidak
	// menahan seluruh halaman.
	liveQuotaProbeTimeout = 6 * time.Second

	// liveQuotaProbeConcurrency membatasi probe serentak ke Google.
	liveQuotaProbeConcurrency = 4
)

var (
	// liveQuotaCacheTTL adalah jendela "segar": selama ini hasil probe dipakai
	// tanpa menyentuh upstream. Jendela kuota 5 jam / 7 hari tidak berubah cepat.
	// Setel LAMROUTER_LIVE_QUOTA_TTL=0 untuk mematikan cache sepenuhnya.
	liveQuotaCacheTTL = 20 * time.Second

	// liveQuotaStaleWindow adalah batas hasil cache masih boleh disajikan
	// seketika sambil disegarkan di latar belakang. Di luar itu (mis. tab yang
	// baru dibuka besok hari) data lama terlalu menyesatkan, jadi halaman
	// menunggu probe yang sebenarnya.
	liveQuotaStaleWindow = 10 * time.Minute
)

func init() {
	if raw := strings.TrimSpace(os.Getenv("LAMROUTER_LIVE_QUOTA_TTL")); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 {
			liveQuotaCacheTTL = time.Duration(secs) * time.Second
		}
	}
}

type liveQuotaCacheEntry struct {
	quotas    []LiveModelQuotaItem
	fetchedAt time.Time
}

type liveQuotaCacheStore struct {
	mu           sync.Mutex
	entries      map[string]liveQuotaCacheEntry
	locks        map[string]*sync.Mutex
	revalidating map[string]bool
}

var liveQuotaCache = &liveQuotaCacheStore{
	entries:      map[string]liveQuotaCacheEntry{},
	locks:        map[string]*sync.Mutex{},
	revalidating: map[string]bool{},
}

// dataKey mengikat hasil probe ke koneksi sekaligus sidik tokennya: kalau
// kredensial sebuah koneksi diganti ke akun lain, kuota akun lama tidak tersaji.
func (s *liveQuotaCacheStore) dataKey(connID, token string) string {
	sum := sha256.Sum256([]byte(token))
	return connID + ":" + hex.EncodeToString(sum[:8])
}

// lock mengunci per koneksi (single-flight). Kuncinya sengaja terpisah dari
// dataKey supaya rotasi access_token tidak memicu probe dobel saat bersamaan.
func (s *liveQuotaCacheStore) lock(connID string) func() {
	s.mu.Lock()
	m, ok := s.locks[connID]
	if !ok {
		m = &sync.Mutex{}
		s.locks[connID] = m
	}
	s.mu.Unlock()

	m.Lock()
	return m.Unlock
}

func (s *liveQuotaCacheStore) get(key string) ([]LiveModelQuotaItem, time.Duration, bool) {
	if liveQuotaCacheTTL <= 0 || key == "" {
		return nil, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok || len(e.quotas) == 0 {
		return nil, 0, false
	}
	return e.quotas, time.Since(e.fetchedAt), true
}

func (s *liveQuotaCacheStore) store(key string, quotas []LiveModelQuotaItem) {
	if key == "" || len(quotas) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = liveQuotaCacheEntry{quotas: quotas, fetchedAt: time.Now()}
}

// beginRevalidate menandai bahwa penyegaran latar belakang sedang berjalan untuk
// satu koneksi, supaya beberapa permintaan yang datang beruntun tidak
// menumpuk-numpuk goroutine probe.
func (s *liveQuotaCacheStore) beginRevalidate(connID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.revalidating[connID] {
		return false
	}
	s.revalidating[connID] = true
	return true
}

func (s *liveQuotaCacheStore) endRevalidate(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.revalidating, connID)
}

func (s *liveQuotaCacheStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = map[string]liveQuotaCacheEntry{}
	s.locks = map[string]*sync.Mutex{}
	s.revalidating = map[string]bool{}
}

// antigravityQuotaProbe dan googleTokenRefresh adalah seam agar tes dapat
// mengganti panggilan jaringan tanpa benar-benar menghubungi Google.
var (
	antigravityQuotaProbe = func(h *RouterAPIHandler, ctx context.Context, token string) ([]LiveModelQuotaItem, error) {
		return h.fetchAntigravityLiveQuotas(ctx, token)
	}
	googleTokenRefresh = func(h *RouterAPIHandler, refreshToken string) (string, error) {
		return h.refreshGoogleOAuthToken(refreshToken)
	}
)

// liveQuotaProbe adalah hasil probe satu akun.
type liveQuotaProbe struct {
	quotas       []LiveModelQuotaItem
	cached       bool
	stale        bool
	revalidating bool
	failed       bool
	age          time.Duration
	err          error
}

// probeLiveQuotaForAccount mengambil kuota live satu akun; lihat komentar
// strategi stale-while-revalidate di kepala berkas.
func (h *RouterAPIHandler) probeLiveQuotaForAccount(ctx context.Context, conn *ProviderConfig, token string) liveQuotaProbe {
	unlock := liveQuotaCache.lock(conn.ID)
	defer unlock()

	key := liveQuotaCache.dataKey(conn.ID, token)

	if quotas, age, ok := liveQuotaCache.get(key); ok {
		if age <= liveQuotaCacheTTL {
			return liveQuotaProbe{quotas: quotas, cached: true, age: age}
		}
		if age <= liveQuotaStaleWindow {
			h.revalidateLiveQuotaAsync(conn, token)
			return liveQuotaProbe{quotas: quotas, cached: true, stale: true, revalidating: true, age: age}
		}
	}

	quotas, err := h.probeLiveQuotaBlocking(ctx, conn, token)
	if err != nil {
		log.Warn("quota", "probe kuota live gagal", "provider", conn.ID, "error", err)
		// Data terakhir yang kita punya tetap lebih berguna daripada kartu kosong.
		if old, age, ok := liveQuotaCache.get(key); ok {
			return liveQuotaProbe{quotas: old, cached: true, stale: true, failed: true, age: age}
		}
		return liveQuotaProbe{failed: true, err: err}
	}

	return liveQuotaProbe{quotas: quotas}
}

// probeLiveQuotaBlocking menjalankan satu probe sungguhan: sekali dengan token
// yang ada, dan sekali lagi setelah refresh HANYA kalau kegagalannya 401.
// Kegagalan 403 atau gangguan jaringan tidak sembuh dengan refresh, dan dulu
// setiap muat halaman tetap membayar satu panggilan OAuth sia-sia untuk itu.
func (h *RouterAPIHandler) probeLiveQuotaBlocking(ctx context.Context, conn *ProviderConfig, token string) ([]LiveModelQuotaItem, error) {
	probeCtx, cancel := context.WithTimeout(ctx, liveQuotaProbeTimeout)
	defer cancel()

	quotas, err := antigravityQuotaProbe(h, probeCtx, token)
	if err != nil && strings.Contains(err.Error(), "HTTP 401") && conn.RefreshToken != nil && *conn.RefreshToken != "" {
		freshToken, refErr := googleTokenRefresh(h, *conn.RefreshToken)
		if refErr == nil && freshToken != "" {
			token = freshToken
			_, _ = h.db.Exec(
				"UPDATE providers SET access_token = ?, last_refreshed_at = ? WHERE id = ?",
				freshToken, time.Now().UnixMilli(), conn.ID)
			quotas, err = antigravityQuotaProbe(h, probeCtx, token)
		}
	}
	if err != nil {
		return nil, err
	}

	liveQuotaCache.store(liveQuotaCache.dataKey(conn.ID, token), quotas)
	return quotas, nil
}

// revalidateLiveQuotaAsync menyegarkan cache di latar belakang: permintaan yang
// sedang berjalan tidak menunggu upstream, tetapi kuota yang disajikan sesudahnya
// (refetch berkala, atau klik tombol Refresh) sudah baru.
func (h *RouterAPIHandler) revalidateLiveQuotaAsync(conn *ProviderConfig, token string) {
	if !liveQuotaCache.beginRevalidate(conn.ID) {
		return
	}

	go func() {
		defer liveQuotaCache.endRevalidate(conn.ID)

		ctx, cancel := context.WithTimeout(context.Background(), liveQuotaProbeTimeout)
		defer cancel()

		// Kunci per akun: jangan menumpuk dengan probe blocking yang sedang jalan.
		unlock := liveQuotaCache.lock(conn.ID)
		defer unlock()

		if _, age, ok := liveQuotaCache.get(liveQuotaCache.dataKey(conn.ID, token)); ok && age <= liveQuotaCacheTTL {
			return
		}
		if _, err := h.probeLiveQuotaBlocking(ctx, conn, token); err != nil {
			log.Warn("quota", "penyegaran latar belakang kuota live gagal", "provider", conn.ID, "error", err)
		}
	}()
}
