package routerapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seedPoolMember inserts one provider row with credential material present.
func seedPoolMember(t *testing.T, h *RouterAPIHandler, id, providerID, name string, enabled int, priority int) {
	t.Helper()
	_, err := h.db.Exec(`INSERT INTO providers
		(id, provider_id, name, category, protocol, base_url, api_key, access_token, refresh_token,
		 account_id, organization_id, token_expires_at, last_refreshed_at, custom_headers,
		 provider_specific_data, priority, enabled, created_at)
		VALUES (?, ?, ?, 'oauth', 'openai', NULL, ?, ?, ?, NULL, NULL, NULL, ?, NULL, NULL, ?, ?, ?)`,
		id, providerID, name,
		"SECRET-API-KEY-"+id, "SECRET-ACCESS-"+id, "SECRET-REFRESH-"+id,
		time.Now().UnixMilli(), priority, enabled, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("seed %s failed: %v", id, err)
	}
}

type poolMember struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Enabled  bool    `json:"enabled"`
	Priority *int    `json:"priority"`
	APIKey   *string `json:"apiKey"`
	Access   *string `json:"accessToken"`
	Refresh  *string `json:"refreshToken"`
}

type catalogEntry struct {
	ID          string       `json:"id"`
	Connections []poolMember `json:"connections"`
	Status      struct {
		State          string `json:"state"`
		ConnectedCount int    `json:"connectedCount"`
	} `json:"status"`
}

func findEntry(entries []catalogEntry, id string) *catalogEntry {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

// The dashboard pool panel is fed by GET /v1/providers and /v1/providers/catalog, which both
// build their entries from buildCatalog(). Those entries must carry their pool members, and
// must never carry the members' tokens.
func TestCatalogExposesPoolMembersWithoutCredentials(t *testing.T) {
	h, _, cleanup := setupTestRouterAPI(t)
	defer cleanup()

	seedPoolMember(t, h, "antigravity-1", "antigravity", "first@example.com", 1, 10)
	seedPoolMember(t, h, "antigravity-2", "antigravity", "second@example.com", 0, 20)
	seedPoolMember(t, h, "river-1", "river", "river@example.com", 1, 5)
	seedPoolMember(t, h, "selfhosted-1", "selfhosted-llm", "selfhosted@example.com", 1, 1)

	pluckList := func(t *testing.T, raw []byte) []catalogEntry {
		t.Helper()
		var payload struct {
			Data []catalogEntry `json:"data"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("list response not decodable: %v", err)
		}
		return payload.Data
	}
	pluckCatalog := func(t *testing.T, raw []byte) []catalogEntry {
		t.Helper()
		var payload struct {
			Categories map[string][]catalogEntry `json:"categories"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("catalog response not decodable: %v", err)
		}
		var all []catalogEntry
		for _, group := range payload.Categories {
			all = append(all, group...)
		}
		return all
	}

	cases := []struct {
		name        string
		call        func(*httptest.ResponseRecorder)
		pluck       func(*testing.T, []byte) []catalogEntry
		wantDynamic bool
	}{
		{
			name: "providers list",
			call: func(w *httptest.ResponseRecorder) {
				h.HandleProvidersList(w, httptest.NewRequest("GET", "/v1/providers", nil))
			},
			pluck: pluckList,
			// saved-only providers are generated dynamically in this path
			wantDynamic: true,
		},
		{
			name: "providers catalog",
			call: func(w *httptest.ResponseRecorder) {
				h.HandleProvidersCatalog(w, httptest.NewRequest("GET", "/v1/providers/catalog", nil))
			},
			pluck: pluckCatalog,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.call(rec)
			raw := rec.Body.Bytes()

			for _, marker := range []string{"SECRET-API-KEY-", "SECRET-ACCESS-", "SECRET-REFRESH-"} {
				if strings.Contains(string(raw), marker) {
					t.Fatalf("%s leaked credential material: %s", tc.name, marker)
				}
			}

			entries := tc.pluck(t, raw)

			antigravity := findEntry(entries, "antigravity")
			if antigravity == nil {
				t.Fatalf("%s: antigravity missing from response", tc.name)
			}
			if len(antigravity.Connections) != 2 {
				t.Fatalf("%s: antigravity has %d pool members, want 2", tc.name, len(antigravity.Connections))
			}
			if antigravity.Status.State != "connected" || antigravity.Status.ConnectedCount != 1 {
				t.Fatalf("%s: antigravity status = %+v, want connected/1", tc.name, antigravity.Status)
			}
			for _, m := range antigravity.Connections {
				if m.Name == "" || m.Priority == nil {
					t.Fatalf("%s: pool member %q lacks name/priority (%+v)", tc.name, m.ID, m)
				}
				if m.APIKey != nil || m.Access != nil || m.Refresh != nil {
					t.Fatalf("%s: pool member %q still carries tokens", tc.name, m.ID)
				}
			}

			river := findEntry(entries, "river")
			if river == nil || len(river.Connections) != 1 {
				t.Fatalf("%s: river entry missing its pool member: %+v", tc.name, river)
			}

			if tc.wantDynamic {
				dynamic := findEntry(entries, "selfhosted-llm")
				if dynamic == nil || len(dynamic.Connections) != 1 {
					t.Fatalf("%s: dynamically generated provider entry is missing its pool member: %+v", tc.name, dynamic)
				}
			}
		})
	}

	// Sanitizing for the catalog must not damage the rows the detail view reads.
	stored, err := h.getAllProviders()
	if err != nil {
		t.Fatalf("getAllProviders failed: %v", err)
	}
	var sawToken bool
	for _, p := range stored {
		if p.APIKey != nil && strings.HasPrefix(*p.APIKey, "SECRET-API-KEY-") {
			sawToken = true
		}
	}
	if !sawToken {
		t.Fatal("sanitizeConnections mutated the stored provider rows")
	}
}
