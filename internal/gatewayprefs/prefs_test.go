package gatewayprefs

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func prefsDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	stmts := []string{
		`CREATE TABLE system_settings (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE request_logs (id TEXT PRIMARY KEY, created_at INTEGER)`,
		`CREATE TABLE requestDetails (id TEXT PRIMARY KEY, timestamp TEXT)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return conn
}

func TestLoadDefaultsWhenNothingStored(t *testing.T) {
	db := prefsDB(t)
	got := Load(db)
	if got != Defaults() {
		t.Fatalf("Load() = %+v, want defaults %+v", got, Defaults())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	db := prefsDB(t)
	want := Defaults()
	want.LogRetentionDays = 7
	want.LoggingLevel = LoggingLevelMetadata
	want.RecordTokenUsage = false
	want.DefaultTemperature = 0.2
	want.DefaultSystemPrompt = "Be brief."
	if _, err := Save(db, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := Load(db); got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestNormalizeValidation(t *testing.T) {
	for _, days := range []int{-1, 3651} {
		if _, err := Normalize(Preferences{LogRetentionDays: days, LoggingLevel: LoggingLevelFull}); err == nil {
			t.Errorf("days=%d should be rejected", days)
		}
	}
	if _, err := Normalize(Preferences{LogRetentionDays: 30, LoggingLevel: "verbose"}); err == nil {
		t.Error("unknown logging level should be rejected")
	}
	// Zero/empty numeric and string fields fall back to defaults; the boolean has
	// no unset state, so it is taken literally (Apply starts from Load).
	got, err := Normalize(Preferences{LoggingLevel: ""})
	if err != nil {
		t.Fatalf("empty values should normalise: %v", err)
	}
	if got.LogRetentionDays != Defaults().LogRetentionDays || got.LoggingLevel != Defaults().LoggingLevel {
		t.Fatalf("Normalize() = %+v, want default window and level", got)
	}
}

func TestLoadFallsBackOnCorruptBlob(t *testing.T) {
	db := prefsDB(t)
	if _, err := db.Exec("INSERT INTO system_settings (key, value) VALUES (?, ?)", SettingsKey, "{not json"); err != nil {
		t.Fatal(err)
	}
	if got := Load(db); got != Defaults() {
		t.Fatalf("corrupt blob should fall back to defaults, got %+v", got)
	}
}

func TestApplyPartialPatchKeepsSiblings(t *testing.T) {
	db := prefsDB(t)
	full := Defaults()
	full.LogRetentionDays = 14
	full.LoggingLevel = LoggingLevelMetadata
	full.RecordTokenUsage = false
	if _, err := Save(db, full); err != nil {
		t.Fatal(err)
	}
	days := 3
	got, err := Apply(db, Patch{LogRetentionDays: &days})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got.LogRetentionDays != 3 || got.LoggingLevel != LoggingLevelMetadata || got.RecordTokenUsage {
		t.Fatalf("partial patch clobbered siblings: %+v", got)
	}
}

func TestPruneRespectsWindow(t *testing.T) {
	db := prefsDB(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	oldMs := now.AddDate(0, 0, -40).UnixMilli()
	newMs := now.AddDate(0, 0, -2).UnixMilli()
	rows := []struct {
		id string
		ts int64
	}{{"old-1", oldMs}, {"new-1", newMs}}
	for _, r := range rows {
		if _, err := db.Exec("INSERT INTO request_logs (id, created_at) VALUES (?, ?)", r.id, r.ts); err != nil {
			t.Fatal(err)
		}
	}
	oldISO := now.AddDate(0, 0, -31).UTC().Format("2006-01-02T15:04:05.000Z")
	newISO := now.AddDate(0, 0, -1).UTC().Format("2006-01-02T15:04:05.000Z")
	for _, r := range []struct{ id, ts string }{{"old-detail", oldISO}, {"new-detail", newISO}} {
		if _, err := db.Exec("INSERT INTO requestDetails (id, timestamp) VALUES (?, ?)", r.id, r.ts); err != nil {
			t.Fatal(err)
		}
	}

	res, err := Prune(db, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.DeletedLogs != 1 || res.DeletedDetails != 1 {
		t.Fatalf("prune result = %+v, want one log and one detail", res)
	}

	var remainingLogs, remainingDetails int
	_ = db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&remainingLogs)
	_ = db.QueryRow("SELECT COUNT(*) FROM requestDetails").Scan(&remainingDetails)
	if remainingLogs != 1 || remainingDetails != 1 {
		t.Fatalf("remaining logs=%d details=%d, want 1/1", remainingLogs, remainingDetails)
	}
}

func TestPruneUsesConfiguredWindow(t *testing.T) {
	db := prefsDB(t)
	if _, err := Save(db, Preferences{LogRetentionDays: 1, LoggingLevel: LoggingLevelFull, RecordTokenUsage: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if _, err := db.Exec("INSERT INTO request_logs (id, created_at) VALUES ('two-days', ?)", now.AddDate(0, 0, -2).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	res, err := Prune(db, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.WindowDays != 1 || res.DeletedLogs != 1 {
		t.Fatalf("window not honoured: %+v", res)
	}
}

func TestLoggingLevelAcceptsDisabled(t *testing.T) {
	db := prefsDB(t)
	if _, err := Apply(db, Patch{LoggingLevel: strPtr(LoggingLevelDisabled)}); err != nil {
		t.Fatalf("disabled level must be accepted: %v", err)
	}
	if got := Load(db).LoggingLevel; got != LoggingLevelDisabled {
		t.Fatalf("level = %q, want disabled", got)
	}
	if _, err := Apply(db, Patch{LoggingLevel: strPtr("verbose")}); err == nil {
		t.Fatal("unknown level must still be rejected")
	}
}

func strPtr(s string) *string { return &s }
