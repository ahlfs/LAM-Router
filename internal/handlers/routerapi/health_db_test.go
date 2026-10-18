package routerapi

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestModelHealthDBOperations(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	if err := h.InitHealthSchema(); err != nil {
		t.Fatalf("InitHealthSchema failed: %v", err)
	}

	// Initial get should be empty
	records, err := h.GetModelHealthRecords()
	if err != nil {
		t.Fatalf("GetModelHealthRecords failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}

	// Upsert healthy model
	now := time.Now().Unix()
	rec := ModelHealthRecord{
		ModelID:       "antigravity/claude-3-7-sonnet",
		ProviderID:    "antigravity",
		Status:        "healthy",
		LatencyMs:     185,
		StatusCode:    200,
		LastError:     "",
		LastCheckedAt: now,
	}
	if err := h.UpsertModelHealth(rec); err != nil {
		t.Fatalf("UpsertModelHealth failed: %v", err)
	}

	records, err = h.GetModelHealthRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %d (err: %v)", len(records), err)
	}
	if records[0].Status != "healthy" || records[0].LatencyMs != 185 {
		t.Errorf("unexpected record content: %+v", records[0])
	}

	// Reset
	if err := h.ResetModelHealth(); err != nil {
		t.Fatalf("ResetModelHealth failed: %v", err)
	}
	records, _ = h.GetModelHealthRecords()
	if len(records) != 1 || records[0].Status != "untested" {
		t.Errorf("expected status 'untested' after reset, got %+v", records)
	}
}
