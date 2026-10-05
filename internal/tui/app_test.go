package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIProvidersTabRendering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.width = 100
	m.height = 30
	m.status = ServerStatus{
		Online:  true,
		Latency: 5 * time.Millisecond,
		BaseURL: "http://127.0.0.1:9898",
	}
	m.providers = []ProviderItem{
		{ID: "antigravity", Name: "Google Antigravity", ConnectedCount: 1, Category: "oauth", Protocol: "openai"},
		{ID: "openai", Name: "OpenAI", ConnectedCount: 0, Category: "api_key", Protocol: "openai"},
	}

	view := m.View()

	if !strings.Contains(view, "LAM-ROUTER CONTROL PLANE") {
		t.Errorf("expected title in view, got:\n%s", view)
	}
	if !strings.Contains(view, "ONLINE") {
		t.Errorf("expected ONLINE badge in view")
	}
	if !strings.Contains(view, "Google Antigravity") {
		t.Errorf("expected Google Antigravity in view")
	}
	if !strings.Contains(view, "[CONNECTED (1 session)]") {
		t.Errorf("expected CONNECTED status badge in view")
	}
}

func TestTUICombosGroupRendering(t *testing.T) {
	// 2 raw fallback rules with same sourceModel 'lopp' should produce 1 grouped Combo
	fallbacks := []FallbackItem{
		{ID: "f1", SourceModel: "lopp", TargetModel: "antigravity/claude-sonnet-4-6", Priority: 1, Enabled: true},
		{ID: "f2", SourceModel: "lopp", TargetModel: "antigravity/gemini-3.8-flash-high", Priority: 2, Enabled: true},
	}

	groups := groupCombos(fallbacks)
	if len(groups) != 1 {
		t.Fatalf("expected 1 combo group, got %d", len(groups))
	}
	if groups[0].Name != "lopp" {
		t.Errorf("expected group name 'lopp', got %s", groups[0].Name)
	}

	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabCombos
	m.fallbacks = fallbacks

	// Default: Collapsed (priority details hidden)
	collapsedView := m.View()
	if !strings.Contains(collapsedView, "Virtual Combos (1 Configured)") {
		t.Errorf("expected 'Virtual Combos (1 Configured)' in view")
	}
	if !strings.Contains(collapsedView, "lopp") {
		t.Errorf("expected 'lopp' in view")
	}
	if strings.Contains(collapsedView, "Priority 1: antigravity/claude-sonnet-4-6") {
		t.Errorf("expected priority details to be hidden when collapsed")
	}

	// Expanded: Priority details visible
	m.expandedCombos["lopp"] = true
	expandedView := m.View()
	if !strings.Contains(expandedView, "Priority 1: antigravity/claude-sonnet-4-6") {
		t.Errorf("expected priority 1 step in expanded view")
	}
	if !strings.Contains(expandedView, "Priority 2: antigravity/gemini-3.8-flash-high") {
		t.Errorf("expected priority 2 step in expanded view")
	}
}

func TestTUIAPIKeysTabRendering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabKeys
	m.apiKeys = []APIKeyItem{
		{
			ID:          "key_123",
			Key:         "lam-live-test-dummy-key",
			Name:        "production-key",
			Enabled:     true,
			UsageTokens: 150000000,
			UsageCost:   150.33,
		},
	}

	view := m.View()
	if !strings.Contains(view, "production-key") {
		t.Errorf("expected key name in view, got:\n%s", view)
	}
	if !strings.Contains(view, "lam-live...-key") {
		t.Errorf("expected masked key in view, got:\n%s", view)
	}
	if !strings.Contains(view, "150.00M tok") {
		t.Errorf("expected formatted token count in view, got:\n%s", view)
	}
	if !strings.Contains(view, "[4] Endpoint & Keys") {
		t.Errorf("expected '[4] Endpoint & Keys' in tab bar, got:\n%s", view)
	}
	if !strings.Contains(view, "ROUTER GATEWAY ENDPOINTS") {
		t.Errorf("expected gateway endpoints info card in view, got:\n%s", view)
	}
}

func TestTUIPlaygroundTabRendering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabPlayground
	m.testingModel = "antigravity/gemini-3.8-flash-high"
	m.models = []ModelItem{
		{ID: "antigravity/gemini-3.8-flash-high", OwnedBy: "antigravity"},
		{ID: "antigravity/claude-sonnet-4-6", OwnedBy: "antigravity"},
	}

	view := m.View()
	if !strings.Contains(view, "gemini-3.8-flash-high") {
		t.Errorf("expected model in view, got:\n%s", view)
	}
	if !strings.Contains(view, "[THINKING]") {
		t.Errorf("expected capability tag in view")
	}
	if !strings.Contains(view, "Prompt Input:") {
		t.Errorf("expected Prompt Input in playground view")
	}
}

func TestTUIEnterKeyHandling(t *testing.T) {
	enterKey := tea.KeyMsg{Type: tea.KeyEnter}

	// 1. Enter on Providers toggles connections dropdown
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabProviders
	m.providers = []ProviderItem{
		{ID: "antigravity", Name: "Google Antigravity", ConnectedCount: 1},
	}
	updatedModel, _ := m.Update(enterKey)
	um := updatedModel.(Model)
	if !um.expandedProviders["antigravity"] {
		t.Errorf("expected expandedProviders['antigravity']=true after enter on provider")
	}

	// 2. Enter on Combos toggles dropdown expander
	m2 := NewModel("http://127.0.0.1:9898")
	m2.activeTab = tabCombos
	m2.fallbacks = []FallbackItem{
		{SourceModel: "lopp", TargetModel: "antigravity/claude-sonnet-4-6", Priority: 1},
	}
	updatedM2, _ := m2.Update(enterKey)
	um2 := updatedM2.(Model)
	if !um2.expandedCombos["lopp"] {
		t.Errorf("expected expandedCombos['lopp']=true, got false")
	}

	// 3. Enter on API Keys toggles detail popup
	m3 := NewModel("http://127.0.0.1:9898")
	m3.activeTab = tabKeys
	m3.apiKeys = []APIKeyItem{
		{ID: "k1", Name: "test", Key: "lam-live-123", Enabled: true},
	}
	updatedM3, _ := m3.Update(enterKey)
	um3 := updatedM3.(Model)
	if !um3.showDetail {
		t.Errorf("expected showDetail=true after enter on api key")
	}
}

func TestTUIComboMultiSelectPriority(t *testing.T) {
	var selected []string

	// 1. Select model 1 -> Priority 1
	selected = toggleComboSelection(selected, "antigravity/claude-sonnet-4-6")
	if p := getComboPriority(selected, "antigravity/claude-sonnet-4-6"); p != 1 {
		t.Errorf("expected priority 1, got %d", p)
	}

	// 2. Select model 2 -> Priority 2
	selected = toggleComboSelection(selected, "antigravity/gemini-3.8-flash-high")
	if p := getComboPriority(selected, "antigravity/gemini-3.8-flash-high"); p != 2 {
		t.Errorf("expected priority 2, got %d", p)
	}

	// 3. Select model 3 -> Priority 3
	selected = toggleComboSelection(selected, "antigravity/gemini-2.5-pro")
	if p := getComboPriority(selected, "antigravity/gemini-2.5-pro"); p != 3 {
		t.Errorf("expected priority 3, got %d", p)
	}

	// 4. Deselect model 2 -> model 3 shifts to Priority 2
	selected = toggleComboSelection(selected, "antigravity/gemini-3.8-flash-high")
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected models, got %d", len(selected))
	}
	if p := getComboPriority(selected, "antigravity/gemini-2.5-pro"); p != 2 {
		t.Errorf("expected model 3 to shift to priority 2, got %d", p)
	}

	// 5. Test float to top ordering
	allModels := []ModelItem{
		{ID: "m1"}, {ID: "m2"}, {ID: "m3"}, {ID: "m4"},
	}
	// selected has ["antigravity/claude-sonnet-4-6", "antigravity/gemini-2.5-pro"]
	ordered := getOrderedModelsForCombo(allModels, []string{"m3", "m1"})
	if len(ordered) != 4 {
		t.Fatalf("expected 4 models, got %d", len(ordered))
	}
	if ordered[0].ID != "m3" || ordered[1].ID != "m1" {
		t.Errorf("expected m3 and m1 floated to top, got %s and %s", ordered[0].ID, ordered[1].ID)
	}
}

func TestTUIComboEditAction(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabCombos
	m.fallbacks = []FallbackItem{
		{ID: "f1", SourceModel: "lopp", TargetModel: "antigravity/claude-sonnet-4-6", Priority: 1},
		{ID: "f2", SourceModel: "lopp", TargetModel: "antigravity/gemini-3.8-flash-high", Priority: 2},
	}

	// Press 'e' on combo
	updatedM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	um := updatedM.(Model)
	if um.comboAction != comboActionEdit {
		t.Errorf("expected comboActionEdit, got %v", um.comboAction)
	}
	if len(um.comboSelected) != 2 {
		t.Fatalf("expected 2 preloaded models in comboSelected, got %d", len(um.comboSelected))
	}
	if um.comboSelected[0] != "antigravity/claude-sonnet-4-6" {
		t.Errorf("expected claude-sonnet as priority 1, got %s", um.comboSelected[0])
	}
	if um.comboSelected[1] != "antigravity/gemini-3.8-flash-high" {
		t.Errorf("expected gemini as priority 2, got %s", um.comboSelected[1])
	}
}

func TestTUIProviderCategoryFiltering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.providers = []ProviderItem{
		{ID: "antigravity", Name: "Google Antigravity", Category: "oauth"},
		{ID: "openai", Name: "OpenAI", Category: "api_key"},
		{ID: "anthropic", Name: "Anthropic", Category: "api_key"},
		{ID: "mimo", Name: "Mimo Free", Category: "free_tier"},
	}

	// 1. All
	m.providerFilter = "all"
	if len(m.getFilteredProviders()) != 4 {
		t.Errorf("expected 4 all providers, got %d", len(m.getFilteredProviders()))
	}

	// 2. OAuth
	m.providerFilter = "oauth"
	oauthProvs := m.getFilteredProviders()
	if len(oauthProvs) != 1 || oauthProvs[0].ID != "antigravity" {
		t.Errorf("expected 1 oauth provider (antigravity), got %d", len(oauthProvs))
	}

	// 3. API Key
	m.providerFilter = "api_key"
	apiProvs := m.getFilteredProviders()
	if len(apiProvs) != 2 {
		t.Errorf("expected 2 api_key providers, got %d", len(apiProvs))
	}
}

func TestTUIAPIKeyCRUDTrigger(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabKeys
	m.apiKeys = []APIKeyItem{
		{ID: "k1", Name: "prod-key", Key: "lam-live-123", Enabled: true},
	}

	// 1. Press 'c' to create key
	updatedM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	um := updatedM.(Model)
	if um.keyAction != keyActionCreate {
		t.Errorf("expected keyActionCreate, got %v", um.keyAction)
	}

	// 2. Press 'e' to edit key
	m.keyAction = keyActionNone
	updatedM2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	um2 := updatedM2.(Model)
	if um2.keyAction != keyActionEdit {
		t.Errorf("expected keyActionEdit, got %v", um2.keyAction)
	}

	// 3. Press 'l' to set limits
	m.keyAction = keyActionNone
	updatedMLim, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	umLim := updatedMLim.(Model)
	if umLim.keyAction != keyActionSetLimits {
		t.Errorf("expected keyActionSetLimits, got %v", umLim.keyAction)
	}

	// 4. Press 'd' to delete key
	m.keyAction = keyActionNone
	updatedM3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	um3 := updatedM3.(Model)
	if um3.keyAction != keyActionDeleteConfirm {
		t.Errorf("expected keyActionDeleteConfirm, got %v", um3.keyAction)
	}
}

func TestTUIExportImportTabRendering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabExportImport
	m.providers = []ProviderItem{
		{ID: "antigravity", Name: "Google Antigravity", ConnectedCount: 1},
	}
	m.latestExportFolder = "/home/user/.lam-router/exports/export-2026-10-05_12-00-00"

	view := m.View()
	if !strings.Contains(view, "[5] Export & Import") {
		t.Errorf("expected '[5] Export & Import' in tab bar, got:\n%s", view)
	}
	if !strings.Contains(view, "PROVIDERS BACKUP & RESTORE UTILITY") {
		t.Errorf("expected utility banner in view, got:\n%s", view)
	}
	if !strings.Contains(view, "FOLDER HASIL EXPORT TERBARU") {
		t.Errorf("expected export folder banner in view, got:\n%s", view)
	}

	// Test Import dialog title
	m.exportImportAction = 1
	dialogView := m.View()
	if !strings.Contains(dialogView, "IMPORT PROVIDERS DARI URL / FILE") {
		t.Errorf("expected import from URL / file dialog in view, got:\n%s", dialogView)
	}

	// Test Export confirmation dialog
	m.exportImportAction = 3
	confirmView := m.View()
	if !strings.Contains(confirmView, "EXPORT CONFIRMATION") {
		t.Errorf("expected export confirmation dialog in view, got:\n%s", confirmView)
	}
}

func TestTUILogsTabRendering(t *testing.T) {
	m := NewModel("http://127.0.0.1:9898")
	m.activeTab = tabLogs
	m.logs = []LogItem{
		{
			ID:            "req_123",
			Model:         "claude-sonnet-4-6",
			StatusCode:    200,
			LatencyMs:     2430,
			TotalTokens:   194,
			EstimatedCost: 0.0011,
			CreatedAt:     time.Now().UnixMilli(),
		},
	}

	view := m.View()
	if !strings.Contains(view, "[6] Logs") {
		t.Errorf("expected '[6] Logs' in tab bar, got:\n%s", view)
	}
	if !strings.Contains(view, "RECENT GATEWAY REQUEST LOGS") {
		t.Errorf("expected request logs header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "claude-sonnet-4-6") {
		t.Errorf("expected model name in logs table")
	}
	if !strings.Contains(view, "200 OK") {
		t.Errorf("expected 200 OK status pill in logs table")
	}
}


