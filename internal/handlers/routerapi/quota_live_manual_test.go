package routerapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

// Manual end-to-end check against the real Antigravity account. Skipped unless
// LR_LIVE_QUOTA is set, because it needs the local router database and network.
func TestLiveAntigravityQuotaCoupling(t *testing.T) {
	if os.Getenv("LR_LIVE_QUOTA") == "" {
		t.Skip("set LR_LIVE_QUOTA=1 to hit the real endpoint")
	}
	home, _ := os.UserHomeDir()
	dbConn, err := sql.Open("sqlite", home+"/.lam-router/db/data.sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	var raw string
	err = dbConn.QueryRow(`SELECT data FROM providerConnections WHERE provider='antigravity' ORDER BY priority LIMIT 1`).Scan(&raw)
	if err != nil {
		t.Fatalf("read connection: %v", err)
	}
	var data struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("parse connection data: %v", err)
	}

	h := &RouterAPIHandler{}
	items, err := h.fetchAntigravityLiveQuotas(context.Background(), data.AccessToken)
	if err != nil {
		t.Fatalf("fetch live quotas: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("no quota rows returned")
	}
	for _, q := range items {
		t.Logf("%-52s pct=%3d status=%-9s blockedByWeekly=%v reset=%q note=%q",
			q.Name, q.PercentageValue, q.Status, q.BlockedByWeekly, q.ResetIn, q.Note)
	}
	for _, q := range items {
		if quotaWindowOf(q.Name) != "5h" {
			continue
		}
		var weekly *LiveModelQuotaItem
		for i := range items {
			if quotaWindowOf(items[i].Name) == "7d" && quotaFamilyOf(items[i].Name) == quotaFamilyOf(q.Name) {
				weekly = &items[i]
			}
		}
		if weekly == nil {
			continue
		}
		if weekly.PercentageValue <= weeklyExhaustedThreshold && (!q.BlockedByWeekly || q.PercentageValue != 0) {
			t.Errorf("%s: weekly is %d%% but 5h still reports %d%% (blocked=%v)", q.Name, weekly.PercentageValue, q.PercentageValue, q.BlockedByWeekly)
		}
	}
}
