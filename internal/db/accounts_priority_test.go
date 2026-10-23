package db

import (
	"testing"
)

// Priority must stay a small rank (1..N) for a provider's queue. It used to be
// assigned MAX(priority)+1 on every failure, so a provider whose accounts
// flapped drifted to 60, 61, 62, ... forever. Renumbering keeps the same
// selection order without the drift.
func TestDemoteConnectionToLowest_RenumbersQueue(t *testing.T) {
	dbConn, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := dbConn.Exec(`CREATE TABLE providers (
		id TEXT PRIMARY KEY,
		provider_id TEXT,
		priority INTEGER
	)`); err != nil {
		t.Fatalf("create providers table: %v", err)
	}

	seed := []struct {
		id  string
		pri int
		upd string
	}{
		{"conn-a", 60, "2026-10-23T09:00:00Z"},
		{"conn-b", 61, "2026-10-23T09:01:00Z"},
		{"conn-c", 62, "2026-10-23T09:02:00Z"},
	}
	for _, s := range seed {
		if _, err := dbConn.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
			VALUES (?, 'antigravity', 'oauth', ?, ?, 1, '{}', ?, ?)`, s.id, s.id, s.pri, s.upd, s.upd); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	repo := NewRepo(dbConn)

	priorities := func() map[string]int {
		rows, err := dbConn.Query(`SELECT id, priority FROM providerConnections WHERE provider = 'antigravity'`)
		if err != nil {
			t.Fatalf("query priorities: %v", err)
		}
		defer rows.Close()
		out := map[string]int{}
		for rows.Next() {
			var id string
			var p int
			if err := rows.Scan(&id, &p); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out[id] = p
		}
		return out
	}

	// Demoting the current best connection moves it to the end and renumbers.
	if err := repo.DemoteConnectionToLowest("conn-a", "antigravity"); err != nil {
		t.Fatalf("demote conn-a: %v", err)
	}
	got := priorities()
	want := map[string]int{"conn-b": 1, "conn-c": 2, "conn-a": 3}
	for id, p := range want {
		if got[id] != p {
			t.Fatalf("after demoting conn-a: %s priority = %d, want %d (all: %v)", id, got[id], p, got)
		}
	}

	// Demoting the new best keeps the queue compact: no growth back to 60+.
	if err := repo.DemoteConnectionToLowest("conn-b", "antigravity"); err != nil {
		t.Fatalf("demote conn-b: %v", err)
	}
	got = priorities()
	want = map[string]int{"conn-c": 1, "conn-a": 2, "conn-b": 3}
	for id, p := range want {
		if got[id] != p {
			t.Fatalf("after demoting conn-b: %s priority = %d, want %d (all: %v)", id, got[id], p, got)
		}
	}

	// A connection that no longer exists is still appended last (stable, no panic).
	if err := repo.DemoteConnectionToLowest("gone", "antigravity"); err != nil {
		t.Fatalf("demote missing connection: %v", err)
	}
	got = priorities()
	for _, p := range got {
		if p > 3 {
			t.Fatalf("priority escaped the 1..N range after demoting a missing conn: %v", got)
		}
	}
}
