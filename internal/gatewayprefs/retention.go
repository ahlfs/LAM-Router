package gatewayprefs

import (
	"database/sql"
	"time"
)

// RetentionInterval is how often the pruner re-checks the audit tables.
const RetentionInterval = 6 * time.Hour

// PruneResult reports what a retention pass removed.
type PruneResult struct {
	DeletedLogs    int64
	DeletedDetails int64
	WindowDays     int
}

// Prune deletes audit rows older than the configured market window. Both tables
// use epoch milliseconds, so the cutoff is computed once in that unit.
func Prune(db *sql.DB, now time.Time) (PruneResult, error) {
	prefs := Load(db)
	days := prefs.LogRetentionDays
	if days <= 0 {
		days = Defaults().LogRetentionDays
	}
	cutoff := now.AddDate(0, 0, -days).UnixMilli()

	result := PruneResult{WindowDays: days}

	res, err := db.Exec("DELETE FROM request_logs WHERE created_at < ?", cutoff)
	if err != nil {
		return result, err
	}
	result.DeletedLogs, _ = res.RowsAffected()

	// requestDetails stores an ISO timestamp string in UTC, not epoch millis.
	isoCutoff := now.AddDate(0, 0, -days).UTC().Format("2006-01-02T15:04:05.000Z")
	if res, err := db.Exec("DELETE FROM requestDetails WHERE timestamp < ?", isoCutoff); err == nil {
		result.DeletedDetails, _ = res.RowsAffected()
	}

	return result, nil
}

// StartRetention runs Prune once at startup and then on a fixed interval. The
// returned stop function ends the loop (tests use it).
func StartRetention(db *sql.DB, interval time.Duration, onRun func(PruneResult)) (stop func()) {
	if db == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = RetentionInterval
	}
	done := make(chan struct{})

	go func() {
		run := func() {
			res, err := Prune(db, time.Now())
			if err == nil && onRun != nil && (res.DeletedLogs > 0 || res.DeletedDetails > 0) {
				onRun(res)
			}
		}
		run()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run()
			case <-done:
				return
			}
		}
	}()

	return func() { close(done) }
}
