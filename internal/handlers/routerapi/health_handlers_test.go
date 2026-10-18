package routerapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func TestHealthHandlers_E2E(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer dbConn.Close()
	dbConn.SetMaxOpenConns(1)

	h := &RouterAPIHandler{db: dbConn}
	if err := h.InitHealthSchema(); err != nil {
		t.Fatalf("failed to init health schema: %v", err)
	}

	// 1. GET /v1/health/models (initial empty db)
	req := httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
	rec := httptest.NewRecorder()
	h.HandleModelsHealthList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var emptySummary HealthSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &emptySummary); err != nil {
		t.Fatalf("failed to unmarshal health summary: %v", err)
	}
	if emptySummary.Models == nil {
		t.Fatalf("expected models slice to be non-nil empty slice, got nil")
	}
	if len(emptySummary.Models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(emptySummary.Models))
	}
	// Verify JSON raw string contains "models":[] and not "models":null
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"models":[]`)) {
		t.Fatalf(`expected body to contain '"models":[]', got: %s`, rec.Body.String())
	}
	if emptySummary.Stats.Total != 0 || emptySummary.Stats.Healthy != 0 {
		t.Fatalf("expected 0 stats on empty db, got %+v", emptySummary.Stats)
	}

	// 2. Seed data: 2 healthy, 1 offline, 1 untested
	now := time.Now().Unix()
	_ = h.UpsertModelHealth(ModelHealthRecord{
		ModelID:       "provider-a/model-1",
		ProviderID:    "provider-a",
		Status:        "healthy",
		LatencyMs:     100,
		StatusCode:    200,
		LastCheckedAt: now,
	})
	_ = h.UpsertModelHealth(ModelHealthRecord{
		ModelID:       "provider-a/model-2",
		ProviderID:    "provider-a",
		Status:        "healthy",
		LatencyMs:     200,
		StatusCode:    200,
		LastCheckedAt: now,
	})
	_ = h.UpsertModelHealth(ModelHealthRecord{
		ModelID:       "provider-b/model-3",
		ProviderID:    "provider-b",
		Status:        "offline",
		LatencyMs:     0,
		StatusCode:    500,
		LastError:     "connection refused",
		LastCheckedAt: now,
	})
	_ = h.UpsertModelHealth(ModelHealthRecord{
		ModelID:       "provider-c/model-4",
		ProviderID:    "provider-c",
		Status:        "untested",
		LatencyMs:     0,
		StatusCode:    0,
		LastCheckedAt: now,
	})

	// 3. GET /v1/health/models with seeded records
	req = httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
	rec = httptest.NewRecorder()
	h.HandleModelsHealthList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var seededSummary HealthSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &seededSummary); err != nil {
		t.Fatalf("failed to unmarshal seeded summary: %v", err)
	}
	if len(seededSummary.Models) != 4 {
		t.Fatalf("expected 4 models, got %d", len(seededSummary.Models))
	}
	if seededSummary.Stats.Total != 4 {
		t.Errorf("expected total 4, got %d", seededSummary.Stats.Total)
	}
	if seededSummary.Stats.Healthy != 2 {
		t.Errorf("expected healthy 2, got %d", seededSummary.Stats.Healthy)
	}
	if seededSummary.Stats.Offline != 1 {
		t.Errorf("expected offline 1, got %d", seededSummary.Stats.Offline)
	}
	if seededSummary.Stats.Untested != 1 {
		t.Errorf("expected untested 1, got %d", seededSummary.Stats.Untested)
	}
	// avg latency of 2 healthy models: (100+200)/2 = 150
	if seededSummary.Stats.AvgLatencyMs != 150 {
		t.Errorf("expected avg_latency_ms 150, got %d", seededSummary.Stats.AvgLatencyMs)
	}

	// 4. Method not allowed tests
	postListReq := httptest.NewRequest(http.MethodPost, "/v1/health/models", nil)
	postListRec := httptest.NewRecorder()
	h.HandleModelsHealthList(postListRec, postListReq)
	if postListRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST /v1/health/models, got %d", postListRec.Code)
	}

	getProbeReq := httptest.NewRequest(http.MethodGet, "/v1/health/models/probe", nil)
	getProbeRec := httptest.NewRecorder()
	h.HandleModelsHealthProbe(getProbeRec, getProbeReq)
	if getProbeRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET /v1/health/models/probe, got %d", getProbeRec.Code)
	}

	getResetReq := httptest.NewRequest(http.MethodGet, "/v1/health/models/reset", nil)
	getResetRec := httptest.NewRecorder()
	h.HandleModelsHealthReset(getResetRec, getResetReq)
	if getResetRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET /v1/health/models/reset, got %d", getResetRec.Code)
	}

	// 5. POST /v1/health/models/probe without model_id -> 400 Bad Request
	emptyProbeReq := httptest.NewRequest(http.MethodPost, "/v1/health/models/probe", bytes.NewReader([]byte(`{}`)))
	emptyProbeReq.Header.Set("Content-Type", "application/json")
	emptyProbeRec := httptest.NewRecorder()
	h.HandleModelsHealthProbe(emptyProbeRec, emptyProbeReq)
	if emptyProbeRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty probe body, got %d", emptyProbeRec.Code)
	}

	// 6. POST /v1/health/models/probe single model
	singleProbeReq := httptest.NewRequest(http.MethodPost, "/v1/health/models/probe", bytes.NewReader([]byte(`{"model_id":"provider-a/model-1"}`)))
	singleProbeReq.Header.Set("Content-Type", "application/json")
	singleProbeRec := httptest.NewRecorder()
	h.HandleModelsHealthProbe(singleProbeRec, singleProbeReq)
	if singleProbeRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for single probe, got %d", singleProbeRec.Code)
	}
	var probedRec ModelHealthRecord
	if err := json.Unmarshal(singleProbeRec.Body.Bytes(), &probedRec); err != nil {
		t.Fatalf("failed to unmarshal single probe result: %v", err)
	}
	if probedRec.ModelID != "provider-a/model-1" {
		t.Errorf("expected model_id provider-a/model-1, got %s", probedRec.ModelID)
	}

	// 7. POST /v1/health/models/probe all models
	allProbeReq := httptest.NewRequest(http.MethodPost, "/v1/health/models/probe", bytes.NewReader([]byte(`{"all":true}`)))
	allProbeReq.Header.Set("Content-Type", "application/json")
	allProbeRec := httptest.NewRecorder()
	h.HandleModelsHealthProbe(allProbeRec, allProbeReq)
	if allProbeRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for probe all, got %d", allProbeRec.Code)
	}
	var allProbeResp struct {
		Status  string              `json:"status"`
		Results []ModelHealthRecord `json:"results"`
	}
	if err := json.Unmarshal(allProbeRec.Body.Bytes(), &allProbeResp); err != nil {
		t.Fatalf("failed to unmarshal probe all resp: %v", err)
	}
	if allProbeResp.Status != "ok" {
		t.Errorf("expected status 'ok', got %s", allProbeResp.Status)
	}
	if allProbeResp.Results == nil {
		t.Errorf("expected results to be non-nil slice")
	}

	// 8. POST /v1/health/models/reset
	resetReq := httptest.NewRequest(http.MethodPost, "/v1/health/models/reset", nil)
	resetRec := httptest.NewRecorder()
	h.HandleModelsHealthReset(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on reset, got %d (body: %s)", resetRec.Code, resetRec.Body.String())
	}

	// Verify all models are now "untested"
	req = httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
	rec = httptest.NewRecorder()
	h.HandleModelsHealthList(rec, req)
	var resetSummary HealthSummaryResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resetSummary)
	if resetSummary.Stats.Untested != resetSummary.Stats.Total {
		t.Errorf("expected all models to be untested after reset, got %+v", resetSummary.Stats)
	}
	if resetSummary.Stats.Healthy != 0 {
		t.Errorf("expected 0 healthy after reset, got %d", resetSummary.Stats.Healthy)
	}
}

func TestHealthHandlers_RouterRegistration(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer dbConn.Close()
	dbConn.SetMaxOpenConns(1)

	r := chi.NewRouter()
	h := &RouterAPIHandler{db: dbConn}
	_ = h.InitHealthSchema()

	// Register routes as routes.go does
	r.HandleFunc("/v1/health/models", func(w http.ResponseWriter, req *http.Request) {
		h.HandleModelsHealthList(w, req)
	})
	r.HandleFunc("/v1/health/models/probe", func(w http.ResponseWriter, req *http.Request) {
		h.HandleModelsHealthProbe(w, req)
	})
	r.HandleFunc("/v1/health/models/reset", func(w http.ResponseWriter, req *http.Request) {
		h.HandleModelsHealthReset(w, req)
	})

	// Test GET /v1/health/models through Chi router
	req := httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via router, got %d", rec.Code)
	}

	// Test POST /v1/health/models/reset through Chi router
	req = httptest.NewRequest(http.MethodPost, "/v1/health/models/reset", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via router, got %d", rec.Code)
	}
}
