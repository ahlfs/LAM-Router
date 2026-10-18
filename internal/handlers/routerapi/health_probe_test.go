package routerapi

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestProbeModel_Extraction(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	_ = h.InitHealthSchema()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Probing a nonexistent model should return offline status without crashing
	rec := h.ProbeModel(ctx, "mock/nonexistent-model")
	if rec.Status != "offline" {
		t.Errorf("expected offline status for nonexistent model, got %s", rec.Status)
	}
	if rec.LastCheckedAt == 0 {
		t.Errorf("expected non-zero LastCheckedAt")
	}
}

func TestProbeAllModels(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer dbConn.Close()

	h := &RouterAPIHandler{db: dbConn}
	_ = h.InitHealthSchema()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ProbeAllModels with fallback models
	records, err := h.ProbeAllModels(ctx, 2)
	if err != nil {
		t.Fatalf("ProbeAllModels failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 fallback models probed, got %d", len(records))
	}
	for _, rec := range records {
		if rec.Status != "offline" && rec.Status != "healthy" {
			t.Errorf("unexpected status %s for model %s", rec.Status, rec.ModelID)
		}
		if rec.LastCheckedAt == 0 {
			t.Errorf("expected non-zero LastCheckedAt for model %s", rec.ModelID)
		}
	}
}
