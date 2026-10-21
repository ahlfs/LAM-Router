package routerapi

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"lamrouter/internal/providers"
)

func newModelContextDB(t *testing.T) *sql.DB {
	t.Helper()

	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	_, err = dbConn.Exec(`CREATE TABLE provider_models (
		provider_id TEXT NOT NULL,
		model_id TEXT NOT NULL,
		context_length INTEGER DEFAULT 0,
		context_source TEXT DEFAULT '',
		is_custom INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		created_at INTEGER DEFAULT 0,
		PRIMARY KEY (provider_id, model_id))`)
	if err != nil {
		t.Fatalf("create provider_models: %v", err)
	}
	return dbConn
}

func modelCtx(t *testing.T, dbConn *sql.DB, providerID, modelID string) int {
	t.Helper()

	var ctx int
	err := dbConn.QueryRow(`SELECT context_length FROM provider_models WHERE provider_id = ? AND model_id = ?`,
		providerID, modelID).Scan(&ctx)
	if err != nil {
		t.Fatalf("read %s/%s: %v", providerID, modelID, err)
	}
	return ctx
}

func modelCtxSource(t *testing.T, dbConn *sql.DB, providerID, modelID string) string {
	t.Helper()

	var src string
	if err := dbConn.QueryRow(`SELECT COALESCE(context_source, '') FROM provider_models WHERE provider_id = ? AND model_id = ?`,
		providerID, modelID).Scan(&src); err != nil {
		t.Fatalf("read source %s/%s: %v", providerID, modelID, err)
	}
	return src
}

// A scan that gets no metadata from upstream must fall back to the model family,
// not to a flat 128k: DeepSeek V4 reports 1M, not 128k.
func TestUpsertDiscoveredModel_UsesFamilyWindow(t *testing.T) {
	dbConn := newModelContextDB(t)
	now := time.Now().UnixMilli()

	upsertDiscoveredModel(dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash-1017", 0, now)

	want, _ := providers.GetModelTokenLimits("river/deepseek-ai/DeepSeek-V4.1-Flash-1017")
	if want != 1048576 {
		t.Fatalf("family table says %d for DeepSeek V4, expected 1048576", want)
	}
	if got := modelCtx(t, dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash-1017"); got != want {
		t.Errorf("stored context = %d, want %d", got, want)
	}
	if src := modelCtxSource(t, dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash-1017"); src != "heuristic" {
		t.Errorf("context_source = %q, want heuristic", src)
	}
}

// A window the provider reports itself is authoritative, even when smaller than
// what a previous scan stored.
func TestUpsertDiscoveredModel_UpstreamWins(t *testing.T) {
	dbConn := newModelContextDB(t)
	now := time.Now().UnixMilli()

	upsertDiscoveredModel(dbConn, "river", "river/some-model", 0, now)
	upsertDiscoveredModel(dbConn, "river", "river/some-model", 65536, now)

	if got := modelCtx(t, dbConn, "river", "river/some-model"); got != 65536 {
		t.Errorf("reported window should win, got %d", got)
	}
	if src := modelCtxSource(t, dbConn, "river", "river/some-model"); src != "upstream" {
		t.Errorf("context_source = %q, want upstream", src)
	}
}

// Re-scanning a provider that reports no metadata must not shrink a window that
// was already discovered correctly.
func TestUpsertDiscoveredModel_DoesNotDowngrade(t *testing.T) {
	dbConn := newModelContextDB(t)
	now := time.Now().UnixMilli()

	upsertDiscoveredModel(dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash", 1048576, now)
	upsertDiscoveredModel(dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash", 0, now)

	if got := modelCtx(t, dbConn, "river", "river/deepseek-ai/DeepSeek-V4.1-Flash"); got != 1048576 {
		t.Errorf("heuristic scan downgraded a known 1M window to %d", got)
	}
}

// An operator override outranks both a rescan and the family table, and the
// repair migration leaves it alone.
func TestUpsertDiscoveredModel_RespectsManualOverride(t *testing.T) {
	dbConn := newModelContextDB(t)
	now := time.Now().UnixMilli()

	const modelID = "river/nvidia/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-NVFP4"
	if _, err := dbConn.Exec(`INSERT INTO provider_models (provider_id, model_id, context_length, context_source, created_at)
		VALUES ('river', ?, 2000000, 'manual', ?)`, modelID, now); err != nil {
		t.Fatalf("seed manual row: %v", err)
	}

	upsertDiscoveredModel(dbConn, "river", modelID, 0, now)
	if got := modelCtx(t, dbConn, "river", modelID); got != 2000000 {
		t.Errorf("heuristic scan overwrote a manual override: %d", got)
	}

	upsertDiscoveredModel(dbConn, "river", modelID, 131072, now)
	if got := modelCtx(t, dbConn, "river", modelID); got != 2000000 {
		t.Errorf("upstream scan overwrote a manual override: %d", got)
	}
	if src := modelCtxSource(t, dbConn, "river", modelID); src != "manual" {
		t.Errorf("context_source = %q, want manual", src)
	}

	repairModelContextWindows(dbConn)
	if got := modelCtx(t, dbConn, "river", modelID); got != 2000000 {
		t.Errorf("repair migration overwrote a manual override: %d", got)
	}
}

// Rows stamped 128k by an older sync are lifted at startup for families that are
// known to be larger, and untouched otherwise.
func TestRepairModelContextWindows_LiftsLegacyRows(t *testing.T) {
	dbConn := newModelContextDB(t)

	rows := []struct {
		provider, model string
		ctx             int
	}{
		{"river", "river/deepseek-ai/DeepSeek-V4.1-Flash-1017", 128000},
		{"antigravity-1", "antigravity/gemini-3-pro", 128000},
		{"river", "river/qwen3-max", 128000},
		{"river", "river/gemini-2.5-pro", 1048576},
	}
	for _, r := range rows {
		if _, err := dbConn.Exec(`INSERT INTO provider_models (provider_id, model_id, context_length) VALUES (?, ?, ?)`,
			r.provider, r.model, r.ctx); err != nil {
			t.Fatalf("seed %s: %v", r.model, err)
		}
	}

	repairModelContextWindows(dbConn)

	cases := []struct {
		provider, model string
		want            int
	}{
		{"river", "river/deepseek-ai/DeepSeek-V4.1-Flash-1017", 1048576},
		{"antigravity-1", "antigravity/gemini-3-pro", 1048576},
		// 128k family: stored default stands, repair only lifts known-bigger ones.
		{"river", "river/qwen3-max", 128000},
		// Already correct: idempotent, no rewrite to a smaller value.
		{"river", "river/gemini-2.5-pro", 1048576},
	}
	for _, c := range cases {
		if got := modelCtx(t, dbConn, c.provider, c.model); got != c.want {
			t.Errorf("%s: context = %d, want %d", c.model, got, c.want)
		}
	}
}
