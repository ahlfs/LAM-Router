package routerapi

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// The dashboard's "connected" badge and the neural pipeline read catalog status. A provider
// config row in `providers` is not a connection: routing resolves upstreams from
// providerConnections WHERE isActive = 1, so counting config rows claimed a provider was live
// while every request failed with "no active connections for provider".
func TestBuildCatalog_ConnectedRequiresActiveRuntimeConnection(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO providers
		(id, provider_id, name, category, protocol, base_url, api_key, priority, enabled, created_at)
		VALUES ('p-antigravity','antigravity','Test Account','oauth','openai',NULL,'tok',1,1,1)`); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	h := &RouterAPIHandler{db: db}
	status := func(id string) map[string]any {
		for _, p := range h.buildCatalog() {
			if p.ID == id {
				return p.Status
			}
		}
		t.Fatalf("provider %s missing from catalog", id)
		return nil
	}

	// Config row without a runtime connection must read as not connected.
	if got := status("antigravity"); got["state"] != "no_connections" || got["connectedCount"] != 0 {
		t.Fatalf("config-only provider reported as connected: %+v", got)
	}

	// An inactive runtime connection still routes nothing.
	if _, err := db.Exec(`INSERT INTO providerConnections
		(id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
		VALUES ('c1','antigravity','oauth','Test Account',1,0,'{}','1','1')`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if got := status("antigravity"); got["state"] != "no_connections" {
		t.Fatalf("inactive connection counted as connected: %+v", got)
	}

	// Activating it makes the provider connected, with the runtime count.
	if _, err := db.Exec(`UPDATE providerConnections SET isActive = 1 WHERE id = 'c1'`); err != nil {
		t.Fatalf("activate connection: %v", err)
	}
	if got := status("antigravity"); got["state"] != "connected" || got["connectedCount"] != 1 {
		t.Fatalf("active connection not reflected: %+v", got)
	}

	// A provider with no config row and no connection never shows up as connected.
	if got := status("river"); got["state"] != "no_connections" {
		t.Fatalf("provider without any row reads connected: %+v", got)
	}
}
