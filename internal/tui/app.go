package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tabIndex int

const (
	tabProviders tabIndex = iota
	tabPlayground
	tabCombos
	tabKeys
	tabExportImport
	tabLogs
)

type providerActionState int

const (
	provActionNone providerActionState = iota
	provActionAddConn
	provActionOAuth
	provActionDeleteConfirm
)

type oauthCallbackMsg struct {
	code  string
	state string
	err   error
}

type comboActionState int

const (
	comboActionNone comboActionState = iota
	comboActionCreate
	comboActionEdit
	comboActionDeleteConfirm
)

type keyActionState int

const (
	keyActionNone keyActionState = iota
	keyActionCreate
	keyActionEdit
	keyActionSetLimits
	keyActionDeleteConfirm
)

type ComboGroup struct {
	Name    string
	Rules   []FallbackItem
	Enabled bool
}

func groupCombos(items []FallbackItem) []ComboGroup {
	order := make([]string, 0)
	groups := make(map[string][]FallbackItem)
	for _, f := range items {
		if _, exists := groups[f.SourceModel]; !exists {
			order = append(order, f.SourceModel)
		}
		groups[f.SourceModel] = append(groups[f.SourceModel], f)
	}

	res := make([]ComboGroup, 0, len(order))
	for _, name := range order {
		rules := groups[name]
		sort.Slice(rules, func(i, j int) bool {
			return rules[i].Priority < rules[j].Priority
		})
		anyEnabled := false
		for _, r := range rules {
			if r.Enabled {
				anyEnabled = true
				break
			}
		}
		res = append(res, ComboGroup{
			Name:    name,
			Rules:   rules,
			Enabled: anyEnabled,
		})
	}
	return res
}

func getComboPriority(selected []string, modelID string) int {
	for i, m := range selected {
		if m == modelID {
			return i + 1
		}
	}
	return 0
}

func toggleComboSelection(selected []string, modelID string) []string {
	for i, m := range selected {
		if m == modelID {
			res := make([]string, 0, len(selected)-1)
			res = append(res, selected[:i]...)
			res = append(res, selected[i+1:]...)
			return res
		}
	}
	return append(selected, modelID)
}

func getOrderedModelsForCombo(allModels []ModelItem, selected []string) []ModelItem {
	selectedMap := make(map[string]bool, len(selected))
	for _, id := range selected {
		selectedMap[id] = true
	}

	modelByID := make(map[string]ModelItem, len(allModels))
	for _, m := range allModels {
		modelByID[m.ID] = m
	}

	res := make([]ModelItem, 0, len(allModels))

	// 1. Add selected models in exact priority order (float to top)
	for _, id := range selected {
		if m, ok := modelByID[id]; ok {
			res = append(res, m)
		} else {
			res = append(res, ModelItem{ID: id, Name: id})
		}
	}

	// 2. Add remaining unselected models below
	for _, m := range allModels {
		if !selectedMap[m.ID] {
			res = append(res, m)
		}
	}

	return res
}

func (m Model) visibleRows(overhead int) int {
	if m.height <= 0 {
		return 10
	}
	r := m.height - overhead
	if r < 3 {
		return 3
	}
	if r > 30 {
		return 30
	}
	return r
}

func (m Model) contentWidth() int {
	w := m.width - 8
	if w < 36 {
		return 36
	}
	return w
}

func (m Model) cardWidth(maxW int) int {
	cw := m.contentWidth()
	if cw < maxW {
		return cw
	}
	return maxW
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

type refreshMsg struct {
	status     ServerStatus
	providers  []ProviderItem
	models     []ModelItem
	fallbacks  []FallbackItem
	apiKeys    []APIKeyItem
	logs       []LogItem
	cacheStats CacheStats
	err        error
}

type testResultMsg struct {
	content string
	latency time.Duration
	err     error
}

type Model struct {
	client     *Client
	activeTab  tabIndex
	width      int
	height     int
	status     ServerStatus
	providers  []ProviderItem
	models     []ModelItem
	fallbacks  []FallbackItem
	apiKeys    []APIKeyItem
	logs       []LogItem
	cacheStats CacheStats
	loading    bool
	err        error

	// Provider Tab State
	providerCursor    int
	providerFilter    string // "all", "oauth", "api_key", "free_tier"
	provAction        providerActionState
	provKeyInput      textinput.Model
	oauthAuthURL      string
	oauthState        string
	oauthProvider     string
	oauthPasteInput   textinput.Model
	expandedProviders map[string]bool
	connCursor        int
	cachedConns       map[string][]ConnectionItem
	showDetail        bool

	// Combo Interactive Actions (Create Multi-select, Edit, Delete, Toggle)
	comboCursor      int
	comboAction      comboActionState
	comboNameInput   textinput.Model
	comboModelCursor int             // cursor in models list during selection
	comboSelected    []string        // ordered slice of selected model IDs (index 0 is Priority 1, index 1 is Priority 2, etc.)
	comboCreateStep  int             // 0: Name, 1: Multi-select Models
	expandedCombos   map[string]bool // expanded state in list view
	actionMsg        string

	// API Key Interactive Actions (Create, Edit, Set Limits, Delete, Toggle)
	keyCursor      int
	keyAction      keyActionState
	keyNameInput   textinput.Model
	keyRateInput   textinput.Model
	keyQuotaInput  textinput.Model
	keyCreditInput textinput.Model
	keyStep        int

	// Export / Import Tab State (Provider saja)
	exportImportAction  int // 0: None, 1: ImportPathOrURL, 2: PasteJSON
	importPathInput     textinput.Model
	importJSONInput     textinput.Model
	exportedJSONContent string
	latestExportFolder  string

	// Logs Tab State
	logCursor int
	logDetail bool

	// Playground state (Models + Tester combined)
	modelCursor  int
	pickingModel bool
	textInput    textinput.Model
	viewport     viewport.Model
	testingModel string
	isTesting    bool
	testResult   string
	testLatency  time.Duration
	testErr      error
	spinner      spinner.Model
}

func NewModel(baseURL string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type prompt here and press Enter (e.g. Ping test or 1+1)..."
	ti.CharLimit = 256
	ti.Width = 60

	cni := textinput.New()
	cni.Placeholder = "e.g. smart-cascade"
	cni.CharLimit = 64
	cni.Width = 40

	pki := textinput.New()
	pki.Placeholder = "sk-... or api key token"
	pki.CharLimit = 128
	pki.Width = 44
	pki.EchoMode = textinput.EchoPassword
	pki.EchoCharacter = '•'

	kni := textinput.New()
	kni.Placeholder = "e.g. production-client or staging"
	kni.CharLimit = 48
	kni.Width = 40

	kri := textinput.New()
	kri.Placeholder = "0 (Unlimited RPM)"
	kri.CharLimit = 10
	kri.Width = 30

	kqi := textinput.New()
	kqi.Placeholder = "0 (Unlimited Tokens)"
	kqi.CharLimit = 15
	kqi.Width = 30

	kci := textinput.New()
	kci.Placeholder = "0 (Unlimited USD)"
	kci.CharLimit = 10
	kci.Width = 30

	opi := textinput.New()
	opi.Placeholder = "Paste callback URL or code here..."
	opi.CharLimit = 1024
	opi.Width = 50

	ipi := textinput.New()
	ipi.Placeholder = "https://... (URL) atau ~/.lam-router/providers-backup.json"
	ipi.CharLimit = 1024
	ipi.Width = 60

	iji := textinput.New()
	iji.Placeholder = `Paste raw providers JSON, e.g. [{"providerId":"...","apiKey":"..."}]`
	iji.CharLimit = 16384
	iji.Width = 60

	vp := viewport.New(60, 8)
	vp.SetContent("Ready for prompt testing. Press Enter to dispatch to router.")

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return Model{
		client:              NewClient(baseURL),
		activeTab:           tabProviders,
		providerFilter:      "all",
		loading:             true,
		textInput:           ti,
		comboNameInput:      cni,
		provKeyInput:        pki,
		oauthPasteInput:     opi,
		keyNameInput:        kni,
		keyRateInput:        kri,
		keyQuotaInput:       kqi,
		keyCreditInput:      kci,
		importPathInput:     ipi,
		importJSONInput:     iji,
		viewport:       vp,
		spinner:        sp,
		testingModel:     "antigravity/gemini-3.8-flash-high",
		expandedCombos:    make(map[string]bool),
		expandedProviders: make(map[string]bool),
		cachedConns:       make(map[string][]ConnectionItem),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchDataCmd(),
		m.spinner.Tick,
	)
}

func (m Model) fetchDataCmd() tea.Cmd {
	return func() tea.Msg {
		status := m.client.CheckHealth()
		provs, _ := m.client.FetchProviders()
		models, _ := m.client.FetchModels()
		fallbacks, _ := m.client.FetchFallbacks()
		keys, _ := m.client.FetchKeys()
		logs, _ := m.client.FetchLogs()
		cache, _ := m.client.FetchCacheStats()

		sort.Slice(provs, func(i, j int) bool {
			if (provs[i].ConnectedCount > 0) != (provs[j].ConnectedCount > 0) {
				return provs[i].ConnectedCount > provs[j].ConnectedCount
			}
			return provs[i].Name < provs[j].Name
		})

		return refreshMsg{
			status:     status,
			providers:  provs,
			models:     models,
			fallbacks:  fallbacks,
			apiKeys:    keys,
			logs:       logs,
			cacheStats: cache,
		}
	}
}

func (m Model) getFilteredProviders() []ProviderItem {
	if m.providerFilter == "" || m.providerFilter == "all" {
		return m.providers
	}
	res := make([]ProviderItem, 0)
	for _, p := range m.providers {
		if p.Category == m.providerFilter {
			res = append(res, p)
		}
	}
	return res
}

func (m Model) runPromptCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		apiKey := ""
		for _, k := range m.apiKeys {
			if k.Enabled && k.Key != "" {
				apiKey = k.Key
				break
			}
		}
		if apiKey == "" {
			apiKey = "test-api-key"
		}

		content, lat, err := m.client.SendPrompt(ctx, apiKey, m.testingModel, prompt)
		return testResultMsg{
			content: content,
			latency: lat,
			err:     err,
		}
	}
}

func (m Model) triggerExportProviders() (Model, tea.Cmd) {
	provs, err := m.client.ExportProviders()
	if err != nil {
		m.actionMsg = fmt.Sprintf("✗ Export failed: %v", err)
		return m, nil
	}
	now := time.Now()
	home, _ := os.UserHomeDir()
	folderName := fmt.Sprintf("export-%s", now.Format("2006-01-02_15-04-05"))
	exportDir := filepath.Join(home, ".lam-router", "exports", folderName)
	if _, err := os.Stat(exportDir); err == nil {
		folderName = fmt.Sprintf("export-%s_%03d", now.Format("2006-01-02_15-04-05"), now.Nanosecond()/1e6)
		exportDir = filepath.Join(home, ".lam-router", "exports", folderName)
	}
	_ = os.MkdirAll(exportDir, 0755)

	targetFile := filepath.Join(exportDir, "providers-backup.json")
	payload := map[string]any{
		"version":    "1.0.0",
		"exportedAt": now.Format(time.RFC3339),
		"folder":     folderName,
		"providers":  provs,
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile(targetFile, b, 0644)

	// Write summary.txt inside the new folder
	summaryFile := filepath.Join(exportDir, "summary.txt")
	summaryTxt := fmt.Sprintf("LAM-Router Provider Export\n"+
		"Created: %s\n"+
		"Total Connections: %d\n"+
		"File: %s\n",
		now.Format(time.RFC1123), len(provs), targetFile)
	_ = os.WriteFile(summaryFile, []byte(summaryTxt), 0644)

	// Also write latest ~/.lam-router/providers-backup.json
	baseDir := filepath.Join(home, ".lam-router")
	_ = os.MkdirAll(baseDir, 0755)
	_ = os.WriteFile(filepath.Join(baseDir, "providers-backup.json"), b, 0644)

	m.exportedJSONContent = string(b)
	m.latestExportFolder = exportDir
	m.actionMsg = fmt.Sprintf("✓ Folder baru dibuat: ~/.lam-router/exports/%s/ (berisi providers-backup.json)", folderName)
	return m, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux, bsd
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func listenForOAuthCallbackCmd(provider string) tea.Cmd {
	return func() tea.Msg {
		port := "1455"
		if provider == "antigravity" {
			port = "51121"
		}

		mux := http.NewServeMux()
		srv := &http.Server{
			Addr:    "127.0.0.1:" + port,
			Handler: mux,
		}
		ch := make(chan oauthCallbackMsg, 1)

		cbHandler := func(w http.ResponseWriter, r *http.Request) {
			qCode := r.URL.Query().Get("code")
			qState := r.URL.Query().Get("state")
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;text-align:center;padding:50px;background:#111;color:#fff;">
<h2 style="color:#10b981;">✓ Authentication Successful!</h2>
<p>LAM-Router has received your credentials.</p>
<p style="color:#888;">You may now close this window and return to your terminal.</p>
</body></html>`))
			ch <- oauthCallbackMsg{code: qCode, state: qState}
			go func() {
				time.Sleep(500 * time.Millisecond)
				_ = srv.Shutdown(context.Background())
			}()
		}

		mux.HandleFunc("/oauth-callback", cbHandler)
		mux.HandleFunc("/auth/"+provider+"/callback", cbHandler)
		mux.HandleFunc("/auth/callback", cbHandler)

		ln, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return oauthCallbackMsg{err: fmt.Errorf("local port %s busy, please paste the redirect URL manually into the prompt", port)}
		}
		go func() {
			_ = srv.Serve(ln)
		}()

		select {
		case res := <-ch:
			return res
		case <-time.After(3 * time.Minute):
			_ = srv.Shutdown(context.Background())
			return oauthCallbackMsg{err: fmt.Errorf("OAuth login timed out")}
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cw := max(36, msg.Width-10)
		m.viewport.Width = cw
		m.viewport.Height = max(4, min(24, msg.Height-18))

		inputW := max(24, min(70, msg.Width-18))
		m.textInput.Width = inputW
		m.comboNameInput.Width = inputW
		m.provKeyInput.Width = inputW
		m.keyNameInput.Width = inputW
		m.keyRateInput.Width = min(20, inputW)
		m.keyQuotaInput.Width = min(20, inputW)
		m.keyCreditInput.Width = min(20, inputW)
		m.importPathInput.Width = inputW
		m.importJSONInput.Width = inputW
		m.oauthPasteInput.Width = inputW

	case refreshMsg:
		m.loading = false
		m.status = msg.status
		m.providers = msg.providers
		m.models = msg.models
		m.fallbacks = msg.fallbacks
		m.apiKeys = msg.apiKeys
		m.logs = msg.logs
		m.cacheStats = msg.cacheStats
		m.err = msg.err
		if len(m.models) > 0 && m.testingModel == "" {
			m.testingModel = m.models[0].ID
		}

	case testResultMsg:
		m.isTesting = false
		m.testLatency = msg.latency
		m.testErr = msg.err
		if msg.err != nil {
			m.testResult = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.testResult = msg.content
		}
		m.viewport.SetContent(m.testResult)

	case oauthCallbackMsg:
		if msg.err != nil {
			m.actionMsg = fmt.Sprintf("✗ OAuth error: %v", msg.err)
			m.provAction = provActionNone
			return m, nil
		}
		email, err := m.client.ExchangeOAuthCallback(m.oauthProvider, msg.code, msg.state, "")
		if err != nil {
			m.actionMsg = fmt.Sprintf("✗ OAuth token exchange failed: %v", err)
		} else {
			m.actionMsg = fmt.Sprintf("✓ Successfully connected OAuth account: %s", email)
		}
		m.provAction = provActionNone
		m.oauthPasteInput.Reset()
		return m, m.fetchDataCmd()

	case tea.KeyMsg:
		isEnter := msg.Type == tea.KeyEnter || msg.String() == "enter" || msg.String() == "ctrl+m" || msg.String() == "\r"
		fProvs := m.getFilteredProviders()
		combos := groupCombos(m.fallbacks)

		// -------------------------------------------------------------
		// 1. Providers Tab Dialog Flows
		// -------------------------------------------------------------
		if m.activeTab == tabProviders && m.provAction == provActionAddConn {
			if msg.String() == "esc" {
				m.provAction = provActionNone
				m.provKeyInput.Reset()
				return m, nil
			}
			if isEnter {
				keyVal := strings.TrimSpace(m.provKeyInput.Value())
				if keyVal != "" && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
					target := fProvs[m.providerCursor]
					err := m.client.AddProviderConnection(target.ID, target.Name, target.Category, keyVal)
					if err != nil {
						m.actionMsg = fmt.Sprintf("✗ Failed to add connection: %v", err)
					} else {
						m.actionMsg = fmt.Sprintf("✓ Successfully added connection for %s", target.Name)
					}
					m.provAction = provActionNone
					m.provKeyInput.Reset()
					return m, m.fetchDataCmd()
				}
				m.provAction = provActionNone
				return m, nil
			}
			var tiCmd tea.Cmd
			m.provKeyInput, tiCmd = m.provKeyInput.Update(msg)
			return m, tiCmd
		}

		if m.activeTab == tabProviders && m.provAction == provActionOAuth {
			if msg.String() == "esc" {
				m.provAction = provActionNone
				m.oauthPasteInput.Reset()
				return m, nil
			}
			if isEnter {
				pasteVal := strings.TrimSpace(m.oauthPasteInput.Value())
				if pasteVal != "" {
					var code, state string
					if strings.Contains(pasteVal, "code=") || strings.Contains(pasteVal, "http") {
						email, err := m.client.ExchangeOAuthCallback(m.oauthProvider, "", m.oauthState, pasteVal)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ OAuth exchange failed: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Successfully connected OAuth account: %s", email)
						}
					} else {
						code = pasteVal
						state = m.oauthState
						email, err := m.client.ExchangeOAuthCallback(m.oauthProvider, code, state, "")
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ OAuth exchange failed: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Successfully connected OAuth account: %s", email)
						}
					}
					m.provAction = provActionNone
					m.oauthPasteInput.Reset()
					return m, m.fetchDataCmd()
				}
				m.provAction = provActionNone
				return m, nil
			}
			var tiCmd tea.Cmd
			m.oauthPasteInput, tiCmd = m.oauthPasteInput.Update(msg)
			return m, tiCmd
		}

		if m.activeTab == tabProviders && m.provAction == provActionDeleteConfirm {
			switch msg.String() {
			case "y", "Y":
				if len(fProvs) > 0 && m.providerCursor < len(fProvs) {
					target := fProvs[m.providerCursor]
					conns := m.cachedConns[target.ID]
					if len(conns) > 0 && m.expandedProviders[target.ID] && m.connCursor < len(conns) {
						targetConn := conns[m.connCursor]
						err := m.client.DeleteProviderConnection(targetConn.ID)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Disconnect failed: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Disconnected session '%s'", targetConn.Name)
						}
					} else {
						err := m.client.DeleteProviderConnection(target.ID)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Disconnect failed: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Disconnected all sessions for %s", target.Name)
						}
					}
				}
				m.provAction = provActionNone
				return m, m.fetchDataCmd()
			case "n", "N", "esc":
				m.provAction = provActionNone
				return m, nil
			}
			return m, nil
		}

		// -------------------------------------------------------------
		// 2. Combos Tab Dialog Flows
		// -------------------------------------------------------------
		if m.activeTab == tabCombos && m.comboAction == comboActionCreate {
			if msg.String() == "esc" {
				m.comboAction = comboActionNone
				m.comboNameInput.Reset()
				m.comboSelected = nil
				m.comboCreateStep = 0
				return m, nil
			}

			if isEnter {
				if m.comboCreateStep == 0 {
					name := strings.TrimSpace(m.comboNameInput.Value())
					if name != "" {
						m.comboCreateStep = 1
						m.comboSelected = nil
						m.comboModelCursor = 0
					}
					return m, nil
				} else if m.comboCreateStep == 1 {
					name := strings.TrimSpace(m.comboNameInput.Value())
					if name != "" && len(m.comboSelected) > 0 {
						for prioIdx, targetModel := range m.comboSelected {
							_ = m.client.CreateFallback(name, targetModel, prioIdx+1)
						}
						count := len(m.comboSelected)
						m.comboAction = comboActionNone
						m.comboNameInput.Reset()
						m.comboSelected = nil
						m.comboCreateStep = 0
						m.actionMsg = fmt.Sprintf("✓ Created combo '%s' with %d cascade models", name, count)
						return m, m.fetchDataCmd()
					}
					return m, nil
				}
			}

			if m.comboCreateStep == 0 {
				var tiCmd tea.Cmd
				m.comboNameInput, tiCmd = m.comboNameInput.Update(msg)
				return m, tiCmd
			} else if m.comboCreateStep == 1 {
				orderedModels := getOrderedModelsForCombo(m.models, m.comboSelected)
				switch msg.String() {
				case " ", "space":
					if len(orderedModels) > 0 && m.comboModelCursor < len(orderedModels) {
						curModel := orderedModels[m.comboModelCursor].ID
						m.comboSelected = toggleComboSelection(m.comboSelected, curModel)
						newOrdered := getOrderedModelsForCombo(m.models, m.comboSelected)
						for idx, om := range newOrdered {
							if om.ID == curModel {
								m.comboModelCursor = idx
								break
							}
						}
					}
					return m, nil
				case "up", "k":
					if m.comboModelCursor > 0 {
						m.comboModelCursor--
					}
					return m, nil
				case "down", "j":
					if m.comboModelCursor < len(orderedModels)-1 {
						m.comboModelCursor++
					}
					return m, nil
				}
			}
		}

		if m.activeTab == tabCombos && m.comboAction == comboActionEdit {
			if msg.String() == "esc" {
				m.comboAction = comboActionNone
				m.comboSelected = nil
				return m, nil
			}

			if isEnter {
				if len(combos) > 0 && m.comboCursor < len(combos) && len(m.comboSelected) > 0 {
					target := combos[m.comboCursor]
					for _, r := range target.Rules {
						_ = m.client.DeleteFallback(r.ID)
					}
					for prioIdx, targetModel := range m.comboSelected {
						_ = m.client.CreateFallback(target.Name, targetModel, prioIdx+1)
					}
					count := len(m.comboSelected)
					m.comboAction = comboActionNone
					m.comboSelected = nil
					m.actionMsg = fmt.Sprintf("✓ Successfully updated combo '%s' with %d cascade models", target.Name, count)
					return m, m.fetchDataCmd()
				}
				return m, nil
			}

			orderedModels := getOrderedModelsForCombo(m.models, m.comboSelected)
			switch msg.String() {
			case " ", "space":
				if len(orderedModels) > 0 && m.comboModelCursor < len(orderedModels) {
					curModel := orderedModels[m.comboModelCursor].ID
					m.comboSelected = toggleComboSelection(m.comboSelected, curModel)
					newOrdered := getOrderedModelsForCombo(m.models, m.comboSelected)
					for idx, om := range newOrdered {
						if om.ID == curModel {
							m.comboModelCursor = idx
							break
						}
					}
				}
				return m, nil
			case "up", "k":
				if m.comboModelCursor > 0 {
					m.comboModelCursor--
				}
				return m, nil
			case "down", "j":
				if m.comboModelCursor < len(orderedModels)-1 {
					m.comboModelCursor++
				}
				return m, nil
			}
			return m, nil
		}

		if m.activeTab == tabCombos && m.comboAction == comboActionDeleteConfirm {
			switch msg.String() {
			case "y", "Y":
				if len(combos) > 0 && m.comboCursor < len(combos) {
					target := combos[m.comboCursor]
					for _, r := range target.Rules {
						_ = m.client.DeleteFallback(r.ID)
					}
					m.actionMsg = fmt.Sprintf("✓ Deleted combo '%s'", target.Name)
					if m.comboCursor > 0 && m.comboCursor >= len(combos)-1 {
						m.comboCursor--
					}
				}
				m.comboAction = comboActionNone
				return m, m.fetchDataCmd()
			case "n", "N", "esc":
				m.comboAction = comboActionNone
				return m, nil
			}
			return m, nil
		}

		// -------------------------------------------------------------
		// 3. API Keys Tab Dialog Flows
		// -------------------------------------------------------------
		if m.activeTab == tabKeys && m.keyAction == keyActionCreate {
			if msg.String() == "esc" {
				m.keyAction = keyActionNone
				m.keyNameInput.Reset()
				m.keyRateInput.Reset()
				m.keyQuotaInput.Reset()
				m.keyCreditInput.Reset()
				m.keyStep = 0
				return m, nil
			}
			if isEnter {
				if m.keyStep == 0 {
					name := strings.TrimSpace(m.keyNameInput.Value())
					if name != "" {
						m.keyStep = 1
						m.keyRateInput.SetValue("0")
						m.keyRateInput.Focus()
					}
					return m, nil
				} else if m.keyStep == 1 {
					m.keyStep = 2
					m.keyQuotaInput.SetValue("0")
					m.keyQuotaInput.Focus()
					return m, nil
				} else if m.keyStep == 2 {
					m.keyStep = 3
					m.keyCreditInput.SetValue("0")
					m.keyCreditInput.Focus()
					return m, nil
				} else if m.keyStep == 3 {
					name := strings.TrimSpace(m.keyNameInput.Value())
					rate, _ := strconv.Atoi(strings.TrimSpace(m.keyRateInput.Value()))
					quota, _ := strconv.Atoi(strings.TrimSpace(m.keyQuotaInput.Value()))
					credit, _ := strconv.ParseFloat(strings.TrimSpace(m.keyCreditInput.Value()), 64)
					created, err := m.client.CreateKey(name, rate, quota, credit)
					if err != nil {
						m.actionMsg = fmt.Sprintf("✗ Failed to create key: %v", err)
					} else {
						m.actionMsg = fmt.Sprintf("✓ Created API Key '%s' (%s)", created.Name, created.Key)
					}
					m.keyAction = keyActionNone
					m.keyNameInput.Reset()
					m.keyRateInput.Reset()
					m.keyQuotaInput.Reset()
					m.keyCreditInput.Reset()
					m.keyStep = 0
					return m, m.fetchDataCmd()
				}
			}

			if m.keyStep == 0 {
				var tiCmd tea.Cmd
				m.keyNameInput, tiCmd = m.keyNameInput.Update(msg)
				return m, tiCmd
			} else if m.keyStep == 1 {
				var tiCmd tea.Cmd
				m.keyRateInput, tiCmd = m.keyRateInput.Update(msg)
				return m, tiCmd
			} else if m.keyStep == 2 {
				var tiCmd tea.Cmd
				m.keyQuotaInput, tiCmd = m.keyQuotaInput.Update(msg)
				return m, tiCmd
			} else if m.keyStep == 3 {
				var tiCmd tea.Cmd
				m.keyCreditInput, tiCmd = m.keyCreditInput.Update(msg)
				return m, tiCmd
			}
		}

		if m.activeTab == tabKeys && m.keyAction == keyActionSetLimits {
			if msg.String() == "esc" {
				m.keyAction = keyActionNone
				m.keyRateInput.Reset()
				m.keyQuotaInput.Reset()
				m.keyCreditInput.Reset()
				m.keyStep = 0
				return m, nil
			}
			if isEnter {
				if m.keyStep == 1 {
					m.keyStep = 2
					m.keyQuotaInput.Focus()
					return m, nil
				} else if m.keyStep == 2 {
					m.keyStep = 3
					m.keyCreditInput.Focus()
					return m, nil
				} else if m.keyStep == 3 {
					if len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
						target := m.apiKeys[m.keyCursor]
						rate, _ := strconv.Atoi(strings.TrimSpace(m.keyRateInput.Value()))
						quota, _ := strconv.Atoi(strings.TrimSpace(m.keyQuotaInput.Value()))
						credit, _ := strconv.ParseFloat(strings.TrimSpace(m.keyCreditInput.Value()), 64)
						err := m.client.UpdateKey(target.ID, target.Name, target.Enabled, rate, quota, credit)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Failed to update limits: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Updated limits for '%s': %d RPM, %s tok, $%.2f", target.Name, rate, formatNumber(int64(quota)), credit)
						}
					}
					m.keyAction = keyActionNone
					m.keyRateInput.Reset()
					m.keyQuotaInput.Reset()
					m.keyCreditInput.Reset()
					m.keyStep = 0
					return m, m.fetchDataCmd()
				}
			}

			if m.keyStep == 1 {
				var tiCmd tea.Cmd
				m.keyRateInput, tiCmd = m.keyRateInput.Update(msg)
				return m, tiCmd
			} else if m.keyStep == 2 {
				var tiCmd tea.Cmd
				m.keyQuotaInput, tiCmd = m.keyQuotaInput.Update(msg)
				return m, tiCmd
			} else if m.keyStep == 3 {
				var tiCmd tea.Cmd
				m.keyCreditInput, tiCmd = m.keyCreditInput.Update(msg)
				return m, tiCmd
			}
		}

		if m.activeTab == tabKeys && m.keyAction == keyActionEdit {
			if msg.String() == "esc" {
				m.keyAction = keyActionNone
				m.keyNameInput.Reset()
				return m, nil
			}
			if isEnter {
				name := strings.TrimSpace(m.keyNameInput.Value())
				if name != "" && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
					target := m.apiKeys[m.keyCursor]
					err := m.client.UpdateKey(target.ID, name, target.Enabled, target.RateLimit, int(target.QuotaLimit), 0)
					if err != nil {
						m.actionMsg = fmt.Sprintf("✗ Failed to update key: %v", err)
					} else {
						m.actionMsg = fmt.Sprintf("✓ Updated key name to '%s'", name)
					}
					m.keyAction = keyActionNone
					m.keyNameInput.Reset()
					return m, m.fetchDataCmd()
				}
				m.keyAction = keyActionNone
				return m, nil
			}
			var tiCmd tea.Cmd
			m.keyNameInput, tiCmd = m.keyNameInput.Update(msg)
			return m, tiCmd
		}

		if m.activeTab == tabKeys && m.keyAction == keyActionDeleteConfirm {
			switch msg.String() {
			case "y", "Y":
				if len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
					target := m.apiKeys[m.keyCursor]
					err := m.client.DeleteKey(target.ID)
					if err != nil {
						m.actionMsg = fmt.Sprintf("✗ Revoke failed: %v", err)
					} else {
						m.actionMsg = fmt.Sprintf("✓ Revoked and deleted key '%s'", target.Name)
					}
					if m.keyCursor > 0 && m.keyCursor >= len(m.apiKeys)-1 {
						m.keyCursor--
					}
				}
				m.keyAction = keyActionNone
				return m, m.fetchDataCmd()
			case "n", "N", "esc":
				m.keyAction = keyActionNone
				return m, nil
			}
			return m, nil
		}

		// -------------------------------------------------------------
		// 3b. Export & Import Tab Dialog Flows
		// -------------------------------------------------------------
		if m.activeTab == tabExportImport && m.exportImportAction == 1 {
			if msg.String() == "esc" {
				m.exportImportAction = 0
				return m, nil
			}
			if isEnter {
				pathVal := strings.TrimSpace(m.importPathInput.Value())
				if pathVal != "" {
					var bytesRead []byte
					var fetchSource string

					if strings.HasPrefix(pathVal, "http://") || strings.HasPrefix(pathVal, "https://") {
						fetchSource = "URL (" + pathVal + ")"
						client := &http.Client{Timeout: 15 * time.Second}
						resp, err := client.Get(pathVal)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Gagal download backup dari URL: %v", err)
							m.exportImportAction = 0
							return m, nil
						}
						defer resp.Body.Close()
						if resp.StatusCode < 200 || resp.StatusCode >= 300 {
							m.actionMsg = fmt.Sprintf("✗ Server URL mengembalikan status HTTP %d", resp.StatusCode)
							m.exportImportAction = 0
							return m, nil
						}
						b, err := io.ReadAll(resp.Body)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Gagal membaca response URL: %v", err)
							m.exportImportAction = 0
							return m, nil
						}
						bytesRead = b
					} else {
						if strings.HasPrefix(pathVal, "~/") {
							home, _ := os.UserHomeDir()
							pathVal = filepath.Join(home, pathVal[2:])
						}
						fetchSource = "file (" + filepath.Base(pathVal) + ")"
						b, err := os.ReadFile(pathVal)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Gagal membaca file: %v", err)
							m.exportImportAction = 0
							return m, nil
						}
						bytesRead = b
					}

					var parsed []map[string]any
					var wrapper struct {
						Providers           []map[string]any `json:"providers"`
						ProviderConnections []map[string]any `json:"providerConnections"`
					}
					if json.Unmarshal(bytesRead, &wrapper) == nil && (len(wrapper.Providers) > 0 || len(wrapper.ProviderConnections) > 0) {
						if len(wrapper.Providers) > 0 {
							parsed = wrapper.Providers
						} else {
							parsed = wrapper.ProviderConnections
						}
					} else {
						_ = json.Unmarshal(bytesRead, &parsed)
					}

					if len(parsed) > 0 {
						count, err := m.client.ImportProviders(parsed)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Import gagal: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Berhasil mengimpor %d koneksi provider dari %s!", count, fetchSource)
						}
					} else {
						m.actionMsg = "✗ Tidak ditemukan konfigurasi provider yang valid dalam data JSON"
					}

					m.exportImportAction = 0
					return m, m.fetchDataCmd()
				}
				m.exportImportAction = 0
				return m, nil
			}
			var tiCmd tea.Cmd
			m.importPathInput, tiCmd = m.importPathInput.Update(msg)
			return m, tiCmd
		}

		if m.activeTab == tabExportImport && m.exportImportAction == 2 {
			if msg.String() == "esc" {
				m.exportImportAction = 0
				m.importJSONInput.Reset()
				return m, nil
			}
			if isEnter {
				jsonVal := strings.TrimSpace(m.importJSONInput.Value())
				if jsonVal != "" {
					var parsed []map[string]any
					var wrapper struct {
						Providers []map[string]any `json:"providers"`
					}
					if json.Unmarshal([]byte(jsonVal), &wrapper) == nil && len(wrapper.Providers) > 0 {
						parsed = wrapper.Providers
					} else {
						_ = json.Unmarshal([]byte(jsonVal), &parsed)
					}
					if len(parsed) > 0 {
						count, err := m.client.ImportProviders(parsed)
						if err != nil {
							m.actionMsg = fmt.Sprintf("✗ Import failed: %v", err)
						} else {
							m.actionMsg = fmt.Sprintf("✓ Successfully imported %d provider connections!", count)
						}
					} else {
						m.actionMsg = "✗ Invalid providers JSON payload"
					}
					m.exportImportAction = 0
					m.importJSONInput.Reset()
					return m, m.fetchDataCmd()
				}
				m.exportImportAction = 0
				return m, nil
			}
			var tiCmd tea.Cmd
			m.importJSONInput, tiCmd = m.importJSONInput.Update(msg)
			return m, tiCmd
		}

		if m.activeTab == tabExportImport && m.exportImportAction == 3 {
			switch msg.String() {
			case "y", "Y", "enter":
				m.exportImportAction = 0
				return m.triggerExportProviders()
			case "n", "N", "esc":
				m.exportImportAction = 0
				m.actionMsg = ""
				return m, nil
			}
			return m, nil
		}

		// -------------------------------------------------------------
		// 4. Normal Enter Handling across Tabs
		// -------------------------------------------------------------
		if isEnter {
			switch m.activeTab {
			case tabProviders:
				if len(fProvs) > 0 && m.providerCursor < len(fProvs) {
					target := fProvs[m.providerCursor]
					m.expandedProviders[target.ID] = !m.expandedProviders[target.ID]
					if m.expandedProviders[target.ID] {
						conns, err := m.client.FetchProviderConnections(target.ID)
						if err == nil {
							m.cachedConns[target.ID] = conns
						} else {
							m.cachedConns[target.ID] = target.Connections
						}
						m.connCursor = 0
					}
					return m, nil
				}

			case tabPlayground:
				if m.pickingModel {
					if len(m.models) > 0 {
						m.testingModel = m.models[m.modelCursor].ID
					}
					m.pickingModel = false
					m.textInput.Focus()
					return m, nil
				}
				if !m.isTesting {
					prompt := strings.TrimSpace(m.textInput.Value())
					if prompt != "" {
						m.isTesting = true
						m.testResult = ""
						m.viewport.SetContent("⏳ Dispatching prompt to upstream model...")
						m.textInput.Reset()
						return m, m.runPromptCmd(prompt)
					}
				}

			case tabCombos:
				if len(combos) > 0 && m.comboCursor < len(combos) {
					target := combos[m.comboCursor]
					m.expandedCombos[target.Name] = !m.expandedCombos[target.Name]
					return m, nil
				}

			case tabKeys:
				if len(m.apiKeys) > 0 {
					m.showDetail = !m.showDetail
				}
				return m, nil

			case tabLogs:
				if len(m.logs) > 0 {
					m.logDetail = !m.logDetail
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "q":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				return m, tea.Quit
			}

		case "esc":
			if m.showDetail {
				m.showDetail = false
				return m, nil
			}
			if m.activeTab == tabPlayground && m.pickingModel {
				m.pickingModel = false
				m.textInput.Focus()
				return m, nil
			}

		case "tab":
			m.activeTab = (m.activeTab + 1) % 6
			m.showDetail = false
			m.pickingModel = false
			m.comboAction = comboActionNone
			m.provAction = provActionNone
			m.keyAction = keyActionNone
			m.exportImportAction = 0
			m.logDetail = false
			if m.activeTab == tabPlayground {
				m.textInput.Focus()
			} else {
				m.textInput.Blur()
			}
			return m, nil

		case "shift+tab":
			m.activeTab = (m.activeTab + 5) % 6
			m.showDetail = false
			m.pickingModel = false
			m.comboAction = comboActionNone
			m.provAction = provActionNone
			m.keyAction = keyActionNone
			m.exportImportAction = 0
			m.logDetail = false
			if m.activeTab == tabPlayground {
				m.textInput.Focus()
			} else {
				m.textInput.Blur()
			}
			return m, nil

		case "1":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabProviders
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Blur()
				return m, nil
			}
		case "2":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabPlayground
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Focus()
				return m, nil
			}
		case "3":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabCombos
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Blur()
				return m, nil
			}
		case "4":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabKeys
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Blur()
				return m, nil
			}
		case "5":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabExportImport
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Blur()
				return m, nil
			}
		case "6":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.activeTab = tabLogs
				m.showDetail = false
				m.pickingModel = false
				m.comboAction = comboActionNone
				m.provAction = provActionNone
				m.keyAction = keyActionNone
				m.exportImportAction = 0
				m.logDetail = false
				m.textInput.Blur()
				return m, nil
			}

		// -------------------------------------------------------------
		// Providers Actions
		// -------------------------------------------------------------
		case "a":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				if target.Category == "oauth" {
					authURL, state, err := m.client.StartOAuthLogin(target.ID)
					if err != nil {
						m.actionMsg = fmt.Sprintf("✗ Failed to start OAuth: %v", err)
						return m, nil
					}
					// Attempt to open browser automatically
					_ = openBrowser(authURL)

					m.provAction = provActionOAuth
					m.oauthProvider = target.ID
					m.oauthAuthURL = authURL
					m.oauthState = state
					m.oauthPasteInput.Reset()
					m.oauthPasteInput.Focus()
					m.actionMsg = ""
					return m, listenForOAuthCallbackCmd(target.ID)
				}
				m.provAction = provActionAddConn
				m.provKeyInput.Reset()
				m.provKeyInput.Focus()
				m.actionMsg = ""
				return m, nil
			}

		case "t":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				conns := m.cachedConns[target.ID]
				apiKey := ""
				targetName := target.Name
				if len(conns) > 0 && m.expandedProviders[target.ID] && m.connCursor < len(conns) {
					apiKey = conns[m.connCursor].APIKey
					targetName = fmt.Sprintf("%s (%s)", target.Name, conns[m.connCursor].Name)
				}
				valid, detail, err := m.client.VerifyProvider(target.ID, target.Protocol, apiKey)
				if err != nil {
					m.actionMsg = fmt.Sprintf("✗ %s test error: %v", targetName, err)
				} else if valid {
					m.actionMsg = fmt.Sprintf("✓ %s verified OK (%s)", targetName, detail)
				} else {
					m.actionMsg = fmt.Sprintf("✗ %s inactive / unconfigured (%s)", targetName, detail)
				}
				return m, nil
			}
			if m.activeTab == tabCombos && len(combos) > 0 && m.comboCursor < len(combos) {
				target := combos[m.comboCursor]
				newStatus := !target.Enabled
				for _, r := range target.Rules {
					_ = m.client.ToggleFallback(r.ID, newStatus)
				}
				m.actionMsg = fmt.Sprintf("✓ Toggled combo '%s' to %v", target.Name, newStatus)
				return m, m.fetchDataCmd()
			}
			if m.activeTab == tabKeys && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
				target := m.apiKeys[m.keyCursor]
				newStatus := !target.Enabled
				err := m.client.UpdateKey(target.ID, target.Name, newStatus, target.RateLimit, int(target.QuotaLimit), target.CreditLimit)
				if err == nil {
					m.actionMsg = fmt.Sprintf("✓ Toggled key '%s' to %v", target.Name, newStatus)
				}
				return m, m.fetchDataCmd()
			}

		case "T", "A":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				conns := m.cachedConns[target.ID]
				if len(conns) == 0 {
					conns, _ = m.client.FetchProviderConnections(target.ID)
					m.cachedConns[target.ID] = conns
				}
				if len(conns) == 0 {
					m.actionMsg = fmt.Sprintf("ℹ No connections to test for %s", target.Name)
					return m, nil
				}
				okCount := 0
				for _, c := range conns {
					valid, _, _ := m.client.VerifyProvider(target.ID, target.Protocol, c.APIKey)
					if valid {
						okCount++
					}
				}
				m.actionMsg = fmt.Sprintf("✓ Tested all %d connections for %s: %d OK, %d failed", len(conns), target.Name, okCount, len(conns)-okCount)
				return m, nil
			}

		case "[":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				if m.expandedProviders[target.ID] && m.connCursor > 0 {
					m.connCursor--
					return m, nil
				}
			}

		case "]":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				conns := m.cachedConns[target.ID]
				if m.expandedProviders[target.ID] && m.connCursor < len(conns)-1 {
					m.connCursor++
					return m, nil
				}
			}

		case "l":
			if m.activeTab == tabKeys && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
				target := m.apiKeys[m.keyCursor]
				m.keyAction = keyActionSetLimits
				m.keyStep = 1
				m.keyRateInput.SetValue(fmt.Sprintf("%d", target.RateLimit))
				m.keyQuotaInput.SetValue(fmt.Sprintf("%d", target.QuotaLimit))
				m.keyCreditInput.SetValue(fmt.Sprintf("%.2f", target.CreditLimit))
				m.keyRateInput.Focus()
				m.actionMsg = ""
				return m, nil
			}

		case "f":
			if m.activeTab == tabProviders {
				switch m.providerFilter {
				case "all":
					m.providerFilter = "oauth"
				case "oauth":
					m.providerFilter = "api_key"
				case "api_key":
					m.providerFilter = "free_tier"
				default:
					m.providerFilter = "all"
				}
				m.providerCursor = 0
				return m, nil
			}

		// -------------------------------------------------------------
		// Combos & Keys Shared Actions
		// -------------------------------------------------------------
		case "p":
			if m.activeTab == tabCombos && len(combos) > 0 && m.comboCursor < len(combos) {
				m.testingModel = combos[m.comboCursor].Name
				m.activeTab = tabPlayground
				m.showDetail = false
				m.pickingModel = false
				m.textInput.Focus()
				m.viewport.SetContent(fmt.Sprintf("Target combo selected: %s (virtual cascade)\nType prompt above and press Enter to test.", m.testingModel))
				return m, nil
			}
			if m.activeTab == tabExportImport {
				m.exportImportAction = 2
				m.importJSONInput.Reset()
				m.importJSONInput.Focus()
				m.actionMsg = ""
				return m, nil
			}

		case "i":
			if m.activeTab == tabExportImport {
				m.exportImportAction = 1
				m.importPathInput.Reset()
				m.importPathInput.Focus()
				m.actionMsg = ""
				return m, nil
			}

		case "x":
			if m.activeTab == tabExportImport {
				m.exportImportAction = 3
				m.actionMsg = ""
				return m, nil
			}

		case "c":
			if m.activeTab == tabCombos {
				m.comboAction = comboActionCreate
				m.comboCreateStep = 0
				m.comboSelected = nil
				m.comboModelCursor = 0
				m.comboNameInput.Reset()
				m.comboNameInput.Focus()
				m.actionMsg = ""
				return m, nil
			}
			if m.activeTab == tabKeys {
				m.keyAction = keyActionCreate
				m.keyStep = 0
				m.keyNameInput.Reset()
				m.keyRateInput.Reset()
				m.keyQuotaInput.Reset()
				m.keyCreditInput.Reset()
				m.keyNameInput.Focus()
				m.actionMsg = ""
				return m, nil
			}

		case "d":
			if m.activeTab == tabProviders && len(fProvs) > 0 && m.providerCursor < len(fProvs) {
				target := fProvs[m.providerCursor]
				if target.ConnectedCount > 0 {
					m.provAction = provActionDeleteConfirm
					m.actionMsg = ""
					return m, nil
				}
			}
			if m.activeTab == tabCombos && len(combos) > 0 {
				m.comboAction = comboActionDeleteConfirm
				m.actionMsg = ""
				return m, nil
			}
			if m.activeTab == tabKeys && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
				m.keyAction = keyActionDeleteConfirm
				m.actionMsg = ""
				return m, nil
			}

		case "e":
			if m.activeTab == tabCombos && len(combos) > 0 && m.comboCursor < len(combos) {
				target := combos[m.comboCursor]
				m.comboAction = comboActionEdit
				m.comboSelected = make([]string, 0, len(target.Rules))
				for _, r := range target.Rules {
					m.comboSelected = append(m.comboSelected, r.TargetModel)
				}
				m.comboModelCursor = 0
				m.actionMsg = ""
				return m, nil
			}
			if m.activeTab == tabKeys && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
				target := m.apiKeys[m.keyCursor]
				m.keyAction = keyActionEdit
				m.keyNameInput.SetValue(target.Name)
				m.keyNameInput.Focus()
				m.actionMsg = ""
				return m, nil
			}
			if m.activeTab == tabExportImport {
				m.exportImportAction = 3
				m.actionMsg = ""
				return m, nil
			}

		case "m":
			if m.activeTab == tabPlayground && (!m.textInput.Focused() || m.textInput.Value() == "") {
				m.pickingModel = !m.pickingModel
				if m.pickingModel {
					m.textInput.Blur()
				} else {
					m.textInput.Focus()
				}
				return m, nil
			}

		case "left":
			if m.activeTab == tabProviders {
				switch m.providerFilter {
				case "free_tier":
					m.providerFilter = "api_key"
				case "api_key":
					m.providerFilter = "oauth"
				case "oauth":
					m.providerFilter = "all"
				default:
					m.providerFilter = "free_tier"
				}
				m.providerCursor = 0
				return m, nil
			}
			if m.activeTab == tabPlayground && !m.pickingModel {
				if m.modelCursor > 0 {
					m.modelCursor--
					m.testingModel = m.models[m.modelCursor].ID
				}
				return m, nil
			}

		case "right":
			if m.activeTab == tabProviders {
				switch m.providerFilter {
				case "all":
					m.providerFilter = "oauth"
				case "oauth":
					m.providerFilter = "api_key"
				case "api_key":
					m.providerFilter = "free_tier"
				default:
					m.providerFilter = "all"
				}
				m.providerCursor = 0
				return m, nil
			}
			if m.activeTab == tabPlayground && !m.pickingModel {
				if m.modelCursor < len(m.models)-1 {
					m.modelCursor++
					m.testingModel = m.models[m.modelCursor].ID
				}
				return m, nil
			}

		case "r":
			if m.activeTab != tabPlayground || !m.textInput.Focused() {
				m.loading = true
				m.actionMsg = ""
				return m, m.fetchDataCmd()
			}

		case "up", "k":
			if m.activeTab != tabPlayground || !m.textInput.Focused() || m.pickingModel {
				m.showDetail = false
				switch m.activeTab {
				case tabProviders:
					if m.providerCursor > 0 {
						m.providerCursor--
					}
				case tabPlayground:
					if m.pickingModel && m.modelCursor > 0 {
						m.modelCursor--
					}
				case tabCombos:
					if m.comboCursor > 0 {
						m.comboCursor--
					}
				case tabKeys:
					if m.keyCursor > 0 {
						m.keyCursor--
					}
				case tabLogs:
					if m.logCursor > 0 {
						m.logCursor--
					}
				}
			}

		case "down", "j":
			if m.activeTab != tabPlayground || !m.textInput.Focused() || m.pickingModel {
				m.showDetail = false
				switch m.activeTab {
				case tabProviders:
					if m.providerCursor < len(fProvs)-1 {
						m.providerCursor++
					}
				case tabPlayground:
					if m.pickingModel && m.modelCursor < len(m.models)-1 {
						m.modelCursor++
					}
				case tabCombos:
					if m.comboCursor < len(combos)-1 {
						m.comboCursor++
					}
				case tabKeys:
					if m.keyCursor < len(m.apiKeys)-1 {
						m.keyCursor++
					}
				case tabLogs:
					if m.logCursor < len(m.logs)-1 {
						m.logCursor++
					}
				}
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.activeTab == tabPlayground && !m.pickingModel {
		var tiCmd, vpCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, tiCmd, vpCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	var s strings.Builder

	// Top Title & Badges Header
	headerLeft := StyleTitle.Render("⚡ LAM-ROUTER CONTROL PLANE")
	var headerBadge string
	if m.status.Online {
		headerBadge = StyleBadge.Render("ONLINE")
	} else {
		headerBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#EF4444")).Padding(0, 1).Render("OFFLINE")
	}
	portInfo := StyleBadgeMuted.Render(fmt.Sprintf(":9898 (%dms)", m.status.Latency.Milliseconds()))
	header := lipgloss.JoinHorizontal(lipgloss.Center, headerLeft, "  ", headerBadge, portInfo)
	s.WriteString(header + "\n\n")

	// Tabs Bar: Responsive labels based on terminal width
	var tabs []string
	if m.width > 0 && m.width < 75 {
		tabs = []string{"[1] Prov", "[2] Play", "[3] Combo", "[4] Keys", "[5] Sync", "[6] Logs"}
	} else if m.width > 0 && m.width < 96 {
		tabs = []string{"[1] Providers", "[2] Playground", "[3] Combos", "[4] Keys", "[5] Backup", "[6] Logs"}
	} else {
		tabs = []string{"[1] Providers", "[2] Playground", "[3] Combos", "[4] Endpoint & Keys", "[5] Export & Import", "[6] Logs"}
	}
	var renderedTabs []string
	for i, t := range tabs {
		if tabIndex(i) == m.activeTab {
			renderedTabs = append(renderedTabs, StyleTabActive.Render(t))
		} else {
			renderedTabs = append(renderedTabs, StyleTabInactive.Render(t))
		}
	}
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...) + "\n\n")

	switch m.activeTab {
	case tabProviders:
		s.WriteString(m.renderProviders())
	case tabPlayground:
		s.WriteString(m.renderPlayground())
	case tabCombos:
		s.WriteString(m.renderCombos())
	case tabKeys:
		s.WriteString(m.renderAPIKeys())
	case tabExportImport:
		s.WriteString(m.renderExportImport())
	case tabLogs:
		s.WriteString(m.renderLogs())
	}

	s.WriteString("\n" + m.renderFooter())

	docStyle := lipgloss.NewStyle().
		MarginLeft(3).
		MarginTop(1)

	return docStyle.Render(s.String())
}

func (m Model) renderProviders() string {
	var b strings.Builder

	// Category Count Stats
	oauthCount, apiKeyCount, freeCount := 0, 0, 0
	for _, p := range m.providers {
		switch p.Category {
		case "oauth":
			oauthCount++
		case "free_tier":
			freeCount++
		default:
			apiKeyCount++
		}
	}

	// Category Filter Badges
	badgeFilter := func(name, key string, count int) string {
		label := fmt.Sprintf("%s (%d)", name, count)
		if m.providerFilter == key {
			return lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(lipgloss.Color("#3B82F6")).Padding(0, 1).Render(label)
		}
		return lipgloss.NewStyle().Foreground(ColorMuted).Padding(0, 1).Render(label)
	}

	filterBar := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render("Category: "),
		badgeFilter("All", "all", len(m.providers)),
		badgeFilter("OAuth", "oauth", oauthCount),
		badgeFilter("API Key", "api_key", apiKeyCount),
		badgeFilter("Free Tier", "free_tier", freeCount),
		lipgloss.NewStyle().Foreground(ColorMuted).Render("  (←/→ or 'f' to filter)"),
	)
	b.WriteString(filterBar + "\n\n")

	if m.actionMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render(m.actionMsg) + "\n\n")
	}

	// 1. Add Connection Dialog
	if m.provAction == provActionAddConn {
		fProvs := m.getFilteredProviders()
		if len(fProvs) > 0 && m.providerCursor < len(fProvs) {
			target := fProvs[m.providerCursor]
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("── ➕ ADD CONNECTION FOR %s ──", target.Name)) + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Enter API Key for %s (%s):", target.Name, target.Category)) + "\n")
			b.WriteString(m.provKeyInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to save connection | Esc to cancel]") + "\n")
			return b.String()
		}
	}

	// 2. OAuth Login Dialog
	if m.provAction == provActionOAuth {
		oauthPort := "1455"
		if m.oauthProvider == "antigravity" {
			oauthPort = "51121"
		}
		shortURL := m.client.BaseURL() + "/v1/auth/" + m.oauthProvider + "/open"
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("── 🔐 OAUTH LOGIN: %s ──", strings.Title(m.oauthProvider))) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render("1. Buka salah satu link ini di browser untuk mengotorisasi akun:") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("👉 Link Cepat (Auto-Redirect): "+shortURL) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("Atau link langsung Google (pastikan ter-copy utuh):") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSecondary).Render(m.oauthAuthURL) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("⏳ Port :%s aktif mendengarkan redirect browser otomatis...", oauthPort)) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("2. Setelah login, copy URL redirect dari address bar browser lalu paste di sini:") + "\n")
		b.WriteString(m.oauthPasteInput.View() + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Tekan Enter untuk submit  |  Esc untuk batal]") + "\n")
		return b.String()
	}

	// 3. Delete Connection Dialog
	if m.provAction == provActionDeleteConfirm {
		fProvs := m.getFilteredProviders()
		if len(fProvs) > 0 && m.providerCursor < len(fProvs) {
			target := fProvs[m.providerCursor]
			conns := m.cachedConns[target.ID]
			desc := fmt.Sprintf("all %d linked accounts for '%s'", target.ConnectedCount, target.Name)
			if len(conns) > 0 && m.expandedProviders[target.ID] && m.connCursor < len(conns) {
				desc = fmt.Sprintf("session '%s' (%s)", conns[m.connCursor].Name, target.Name)
			}
			confirmBox := StyleCardHighlight.Width(66).Render(
				fmt.Sprintf("⚠️  DISCONNECT CONFIRMATION\n\nAre you sure you want to disconnect %s?\n\n[Press 'y' to Confirm Disconnect  |  'n' / Esc to Cancel]",
					desc),
			)
			b.WriteString(confirmBox + "\n")
			return b.String()
		}
	}

	fProvs := m.getFilteredProviders()
	if len(fProvs) == 0 {
		b.WriteString(StyleMutedItem.Render("No upstream drivers found in this category.\n"))
		return b.String()
	}

	maxShow := m.visibleRows(11)
	start := max(0, min(m.providerCursor-maxShow/2, len(fProvs)-maxShow))
	end := min(start+maxShow, len(fProvs))

	for i := start; i < end; i++ {
		p := fProvs[i]

		cursor := "  "
		style := StyleNormalItem
		if i == m.providerCursor {
			cursor = "❯ "
			style = StyleSelectedItem
		}

		nameW := 26
		if m.width >= 110 {
			nameW = 34
		} else if m.width < 85 {
			nameW = 18
		}
		pName := truncate(p.Name, nameW)

		statusPill := lipgloss.NewStyle().Foreground(ColorMuted).Render("[NOT CONFIGURED]")
		if p.ConnectedCount > 0 {
			if m.width < 85 {
				statusPill = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("[CONN: %d]", p.ConnectedCount))
			} else {
				statusPill = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("[CONNECTED (%d %s)]", p.ConnectedCount, plural(p.ConnectedCount, "session", "sessions")))
			}
		}

		cat := fmt.Sprintf("(%s / %s)", p.Category, p.Protocol)
		if m.width < 85 {
			cat = fmt.Sprintf("(%s)", p.Category)
		}

		isExp := m.expandedProviders[p.ID]
		expandPill := lipgloss.NewStyle().Foreground(ColorMuted).Render("▸ [Enter: Conns]")
		if isExp {
			expandPill = lipgloss.NewStyle().Foreground(ColorPrimary).Render("▾ [Enter: Close]")
		}

		line := fmt.Sprintf("%s%-*s %-16s %s  %s", cursor, nameW, pName, cat, statusPill, expandPill)
		b.WriteString(style.Render(line) + "\n")

		if isExp {
			conns := m.cachedConns[p.ID]
			if len(conns) == 0 && len(p.Connections) > 0 {
				conns = p.Connections
			}

			if len(conns) > 0 {
				for cIdx, c := range conns {
					connCursor := "    ├─   "
					if cIdx == m.connCursor && i == m.providerCursor {
						connCursor = "    ├─ ❯ "
					}
					cStatus := lipgloss.NewStyle().Foreground(ColorSuccess).Render("[ACTIVE]")
					if !c.Enabled {
						cStatus = lipgloss.NewStyle().Foreground(ColorMuted).Render("[DISABLED]")
					}
					cLine := fmt.Sprintf("%s[%d] %-30s Priority: %-2d %s", connCursor, cIdx+1, c.Name, c.Priority, cStatus)
					b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(cLine) + "\n")
				}
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("    │\n    └─ Actions: [a] Add Con | [d] Disconnect Sel | [t] Test Sel | [T] Test All | [ [ / ] ] Switch Con") + "\n\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("    ├─ (No active connections in pool)") + "\n")
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("    └─ Actions: [a] Add Connection") + "\n\n")
			}
		}
	}

	if m.showDetail && len(fProvs) > 0 {
		sel := fProvs[m.providerCursor]
		detailContent := fmt.Sprintf(
			"Provider: %s\nID: %s\nCategory: %s | Protocol: %s\nActive Sessions: %d\nStatus: %s\n\n[Hotkeys: 'a' Add Connection | 'd' Disconnect | 't' Test | Enter/Esc Close]",
			sel.Name, sel.ID, sel.Category, sel.Protocol, sel.ConnectedCount, sel.StatusState,
		)
		detailBox := StyleCardHighlight.Width(68).Render(detailContent)
		b.WriteString("\n" + detailBox + "\n")
	}

	return b.String()
}

func (m Model) renderPlayground() string {
	var b strings.Builder

	// Model Header & Selector Card
	caps := ""
	if strings.Contains(strings.ToLower(m.testingModel), "high") || strings.Contains(strings.ToLower(m.testingModel), "thinking") {
		caps += " [THINKING]"
	}
	if strings.Contains(strings.ToLower(m.testingModel), "flash") || strings.Contains(strings.ToLower(m.testingModel), "sonnet") {
		caps += " [VISION]"
	}

	targetHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("🎯 TARGET MODEL: ") +
		lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(m.testingModel) +
		lipgloss.NewStyle().Foreground(ColorSecondary).Render(caps)

	controlHints := lipgloss.NewStyle().Foreground(ColorMuted).Render(
		fmt.Sprintf(" [← / →: Switch Model  |  m: Browse all %d models]", len(m.models)),
	)
	b.WriteString(targetHeader + controlHints + "\n\n")

	// If model picker is active, show the interactive list
	if m.pickingModel {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("── Select Model (↑/↓ to navigate, Enter to select, Esc to close) ──") + "\n")
		maxShow := 8
		start := max(0, min(m.modelCursor-maxShow/2, len(m.models)-maxShow))
		end := min(start+maxShow, len(m.models))

		for i := start; i < end; i++ {
			mod := m.models[i]
			cursor := "  "
			style := StyleNormalItem
			if i == m.modelCursor {
				cursor = "❯ "
				style = StyleSelectedItem
			}
			line := fmt.Sprintf("%s%-38s %-12s", cursor, mod.ID, "("+mod.OwnedBy+")")
			b.WriteString(style.Render(line) + "\n")
		}
		b.WriteString("\n")
	}

	// Prompt Input Area
	b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render("Prompt Input:") + "\n")
	b.WriteString(m.textInput.View() + "\n\n")

	// Response Viewport Area
	if m.isTesting {
		b.WriteString(m.spinner.View() + " Streaming response from router...\n\n")
	} else if m.testLatency > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Render(fmt.Sprintf("✓ Response received in %dms:\n", m.testLatency.Milliseconds())))
	}

	box := StyleCard.Width(m.viewport.Width).Render(m.viewport.View())
	b.WriteString(box + "\n")

	return b.String()
}

func (m Model) renderCombos() string {
	var b strings.Builder
	combos := groupCombos(m.fallbacks)

	// Action Feedback Message
	if m.actionMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render(m.actionMsg) + "\n\n")
	}

	// 1. Creation Form View (Step 1: Name, Step 2: Multi-select Checkbox List)
	if m.comboAction == comboActionCreate {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("── ➕ CREATE NEW VIRTUAL COMBO ──") + "\n\n")
		if m.comboCreateStep == 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("Step 1/2: Enter Combo Virtual Model Name (e.g. smart-cascade):") + "\n")
			b.WriteString(m.comboNameInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed to model selection | Esc to cancel]") + "\n")
		} else if m.comboCreateStep == 1 {
			name := m.comboNameInput.Value()
			headerText := fmt.Sprintf("Step 2/2 for '%s': Select models in order of failover priority", name)
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(headerText) + "\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("(Press [Space] to select/deselect and set priority, [Enter] when done)") + "\n\n")

			orderedModels := getOrderedModelsForCombo(m.models, m.comboSelected)
			maxShow := 8
			start := max(0, min(m.comboModelCursor-maxShow/2, len(orderedModels)-maxShow))
			end := min(start+maxShow, len(orderedModels))

			for i := start; i < end; i++ {
				mod := orderedModels[i]
				cursor := "  "
				style := StyleNormalItem
				if i == m.comboModelCursor {
					cursor = "❯ "
					style = StyleSelectedItem
				}

				prio := getComboPriority(m.comboSelected, mod.ID)
				var checkPill string
				if prio > 0 {
					checkPill = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("[✓ #%d]", prio))
				} else {
					checkPill = lipgloss.NewStyle().Foreground(ColorMuted).Render("[    ]")
				}

				// Visual separator between selected and unselected models
				if prio == 0 && i == len(m.comboSelected) && len(m.comboSelected) > 0 {
					b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render("    ──────────────────────────────────────────────────") + "\n")
				}

				line := fmt.Sprintf("%s%s %-40s %-12s", cursor, checkPill, mod.ID, "("+mod.OwnedBy+")")
				b.WriteString(style.Render(line) + "\n")
			}
			b.WriteString("\n")

			// Live Cascade Chain Preview Box
			if len(m.comboSelected) > 0 {
				var chainParts []string
				for idx, sm := range m.comboSelected {
					chainParts = append(chainParts, fmt.Sprintf("#%d: %s", idx+1, sm))
				}
				chainText := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("⛓️  Cascade Chain: ") +
					strings.Join(chainParts, " ➔ ")
				b.WriteString(chainText + "\n\n")
				b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(
					fmt.Sprintf("[Space: Toggle Select  |  Enter: CREATE COMBO (%d models)  |  Esc: Cancel]", len(m.comboSelected)),
				) + "\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Render("⛓️  Cascade Chain: (No models selected yet. Navigate with ↑/↓ and press [Space] to select models.)") + "\n\n")
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Space: Select Model  |  Esc: Cancel]") + "\n")
			}
		}
		return b.String()
	}

	// 2. Edit Form View (Multi-select)
	if m.comboAction == comboActionEdit && len(combos) > 0 && m.comboCursor < len(combos) {
		target := combos[m.comboCursor]
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("── ✏️ EDIT VIRTUAL COMBO: %s ──", target.Name)) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render("Adjust cascade models and failover priority:") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("(Press [Space] to select/deselect and set priority, [Enter] to save changes)") + "\n\n")

		orderedModels := getOrderedModelsForCombo(m.models, m.comboSelected)
		maxShow := 8
		start := max(0, min(m.comboModelCursor-maxShow/2, len(orderedModels)-maxShow))
		end := min(start+maxShow, len(orderedModels))

		for i := start; i < end; i++ {
			mod := orderedModels[i]
			cursor := "  "
			style := StyleNormalItem
			if i == m.comboModelCursor {
				cursor = "❯ "
				style = StyleSelectedItem
			}

			prio := getComboPriority(m.comboSelected, mod.ID)
			var checkPill string
			if prio > 0 {
				checkPill = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("[✓ #%d]", prio))
			} else {
				checkPill = lipgloss.NewStyle().Foreground(ColorMuted).Render("[    ]")
			}

			// Visual separator between selected and unselected models
			if prio == 0 && i == len(m.comboSelected) && len(m.comboSelected) > 0 {
				b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render("    ──────────────────────────────────────────────────") + "\n")
			}

			line := fmt.Sprintf("%s%s %-40s %-12s", cursor, checkPill, mod.ID, "("+mod.OwnedBy+")")
			b.WriteString(style.Render(line) + "\n")
		}
		b.WriteString("\n")

		// Live Cascade Chain Preview Box
		if len(m.comboSelected) > 0 {
			var chainParts []string
			for idx, sm := range m.comboSelected {
				chainParts = append(chainParts, fmt.Sprintf("#%d: %s", idx+1, sm))
			}
			chainText := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("⛓️  Updated Cascade Chain: ") +
				strings.Join(chainParts, " ➔ ")
			b.WriteString(chainText + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(
				fmt.Sprintf("[Space: Toggle Select  |  Enter: SAVE CHANGES (%d models)  |  Esc: Cancel]", len(m.comboSelected)),
			) + "\n")
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Render("⛓️  Updated Cascade Chain: (No models selected. At least 1 model required.)") + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Space: Select Model  |  Esc: Cancel]") + "\n")
		}
		return b.String()
	}

	// 3. Delete Confirmation View
	if m.comboAction == comboActionDeleteConfirm && len(combos) > 0 {
		target := combos[m.comboCursor]
		confirmBox := StyleCardHighlight.Width(66).Render(
			fmt.Sprintf("⚠️  DELETE COMBO CONFIRMATION\n\nAre you sure you want to permanently delete combo '%s'?\nThis will remove all %d fallback cascade steps.\n\n[Press 'y' to Confirm  |  'n' / Esc to Cancel]",
				target.Name, len(target.Rules)),
		)
		b.WriteString(confirmBox + "\n")
		return b.String()
	}

	// 4. Normal Combos List View
	header := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(
		fmt.Sprintf("Virtual Combos (%d Configured) — [Enter] Expand/Collapse  [c] Create  [e] Edit  [d] Delete  [t] Toggle  [p] Test in Playground:", len(combos)),
	)
	b.WriteString(header + "\n\n")

	if len(combos) == 0 {
		b.WriteString(StyleMutedItem.Render("No virtual combos created yet. Press 'c' to create your first combo!\n"))
		return b.String()
	}

	for i, c := range combos {
		cursor := "  "
		style := StyleNormalItem
		if i == m.comboCursor {
			cursor = "❯ "
			style = StyleSelectedItem
		}

		status := lipgloss.NewStyle().Foreground(ColorSuccess).Render("[ACTIVE]")
		if !c.Enabled {
			status = lipgloss.NewStyle().Foreground(ColorMuted).Render("[DISABLED]")
		}

		isExpanded := m.expandedCombos[c.Name]
		var expandPill string
		if isExpanded {
			expandPill = lipgloss.NewStyle().Foreground(ColorPrimary).Render("▾ [Enter: Collapse]")
		} else {
			expandPill = lipgloss.NewStyle().Foreground(ColorMuted).Render("▸ [Enter: Expand dropdown]")
		}

		headLine := fmt.Sprintf("%s%-18s %s (%d %s)  %s",
			cursor, c.Name, status, len(c.Rules), plural(len(c.Rules), "model", "models"), expandPill)
		b.WriteString(style.Render(headLine) + "\n")

		// ONLY render dropdown cascade models if expanded!
		if isExpanded {
			for rIdx, r := range c.Rules {
				treePrefix := "    ├─"
				if rIdx == len(c.Rules)-1 {
					treePrefix = "    └─"
				}
				ruleLine := fmt.Sprintf("%s Priority %d: %-36s", treePrefix, r.Priority, r.TargetModel)
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render(ruleLine) + "\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAPIKeys() string {
	var b strings.Builder

	if m.actionMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render(m.actionMsg) + "\n\n")
	}

	// 1. Create Key Dialog
	if m.keyAction == keyActionCreate {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("── ➕ CREATE CLIENT API KEY ──") + "\n\n")
		if m.keyStep == 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("Step 1/4: Enter Key Name (e.g. app-prod or testing):") + "\n")
			b.WriteString(m.keyNameInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed | Esc to cancel]") + "\n")
		} else if m.keyStep == 1 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 2/4 for '%s': Rate Limit (RPM, requests per minute, 0 = unlimited):", m.keyNameInput.Value())) + "\n")
			b.WriteString(m.keyRateInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed | Esc to cancel]") + "\n")
		} else if m.keyStep == 2 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 3/4 for '%s': Quota Limit in Tokens (0 = unlimited):", m.keyNameInput.Value())) + "\n")
			b.WriteString(m.keyQuotaInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed | Esc to cancel]") + "\n")
		} else if m.keyStep == 3 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 4/4 for '%s': Credit Limit in USD $ (0 = unlimited):", m.keyNameInput.Value())) + "\n")
			b.WriteString(m.keyCreditInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to GENERATE KEY | Esc to cancel]") + "\n")
		}
		return b.String()
	}

	// 2. Set Limits Dialog
	if m.keyAction == keyActionSetLimits && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
		target := m.apiKeys[m.keyCursor]
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("── ⚙️ CONFIGURE LIMITS FOR KEY: %s ──", target.Name)) + "\n\n")
		if m.keyStep == 1 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 1/3: Rate Limit RPM (Current: %d RPM, 0 = unlimited):", target.RateLimit)) + "\n")
			b.WriteString(m.keyRateInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed | Esc to cancel]") + "\n")
		} else if m.keyStep == 2 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 2/3: Quota Limit in Tokens (Current: %d, 0 = unlimited):", target.QuotaLimit)) + "\n")
			b.WriteString(m.keyQuotaInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to proceed | Esc to cancel]") + "\n")
		} else if m.keyStep == 3 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("Step 3/3: Credit Limit in USD $ (Current: $%.2f, 0 = unlimited):", target.CreditLimit)) + "\n")
			b.WriteString(m.keyCreditInput.View() + "\n\n")
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to SAVE LIMITS | Esc to cancel]") + "\n")
		}
		return b.String()
	}

	// 3. Edit Key Dialog
	if m.keyAction == keyActionEdit && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
		target := m.apiKeys[m.keyCursor]
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf("── ✏️ EDIT API KEY: %s ──", target.Name)) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("New Key Name:") + "\n")
		b.WriteString(m.keyNameInput.View() + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to save name | Esc to cancel]") + "\n")
		return b.String()
	}

	// 4. Delete Key Dialog
	if m.keyAction == keyActionDeleteConfirm && len(m.apiKeys) > 0 && m.keyCursor < len(m.apiKeys) {
		target := m.apiKeys[m.keyCursor]
		confirmBox := StyleCardHighlight.Width(66).Render(
			fmt.Sprintf("⚠️  REVOKE API KEY CONFIRMATION\n\nAre you sure you want to permanently revoke key '%s'?\nKey: %s\n\n[Press 'y' to Confirm Revoke  |  'n' / Esc to Cancel]",
				target.Name, target.Key),
		)
		b.WriteString(confirmBox + "\n")
		return b.String()
	}

	// 5. Normal Endpoint & Keys View
	endpointBox := StyleCardHighlight.Width(min(68, m.width-12)).Render(
		fmt.Sprintf("🌐 ROUTER GATEWAY ENDPOINTS (OpenAI Compatible)\n\n"+
			"• Base URL:       %s/v1\n"+
			"• Chat Endpoint:  %s/v1/chat/completions\n"+
			"• Models Catalog: %s/v1/models\n"+
			"• Auth Header:    Authorization: Bearer <your_key_here>",
			m.status.BaseURL, m.status.BaseURL, m.status.BaseURL),
	)
	b.WriteString(endpointBox + "\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(
		fmt.Sprintf("Client Keys (%d Configured) — [c] Create  [e] Edit Name  [l] Set Limits  [d] Revoke  [t] Toggle  [Enter] Details:", len(m.apiKeys))) + "\n\n")

	if len(m.apiKeys) == 0 {
		b.WriteString(StyleMutedItem.Render("No API keys found in database. Press 'c' to create one!\n"))
		return b.String()
	}

	maxShow := m.visibleRows(14)
	start := max(0, min(m.keyCursor-maxShow/2, len(m.apiKeys)-maxShow))
	end := min(start+maxShow, len(m.apiKeys))

	for i := start; i < end; i++ {
		k := m.apiKeys[i]
		cursor := "  "
		style := StyleNormalItem
		if i == m.keyCursor {
			cursor = "❯ "
			style = StyleSelectedItem
		}

		status := lipgloss.NewStyle().Foreground(ColorSuccess).Render("[ACTIVE]")
		if !k.Enabled {
			status = lipgloss.NewStyle().Foreground(ColorMuted).Render("[DISABLED]")
		}

		maskedKey := k.Key
		if len(maskedKey) > 16 {
			maskedKey = maskedKey[:8] + "..." + maskedKey[len(maskedKey)-4:]
		}

		tokensStr := formatNumber(k.UsageTokens) + " tok"
		costStr := fmt.Sprintf("$%.2f", k.UsageCost)

		nameW := 20
		if m.width >= 110 {
			nameW = 26
		} else if m.width > 0 && m.width < 85 {
			nameW = 14
		}
		kName := truncate(k.Name, nameW)

		line := fmt.Sprintf("%s%-*s %-18s %-12s %-8s %s", cursor, nameW, kName, maskedKey, tokensStr, costStr, status)
		b.WriteString(style.Render(line) + "\n")
	}

	if m.showDetail && len(m.apiKeys) > 0 {
		sel := m.apiKeys[m.keyCursor]
		detailContent := fmt.Sprintf(
			"Key Name: %s\nID: %s\nFull Key: %s\nStatus: %s\nUsage Tokens: %s\nUsage Cost: $%.4f\nRate Limit: %d RPM\nQuota Limit: %s tok\nCredit Limit: $%.2f\n\n[Hotkeys: 'e' Edit Name | 'l' Set Limits | 'd' Revoke | 't' Toggle | Enter/Esc Close]",
			sel.Name, sel.ID, sel.Key,
			map[bool]string{true: "ACTIVE", false: "DISABLED"}[sel.Enabled],
			formatNumber(sel.UsageTokens), sel.UsageCost, sel.RateLimit, formatNumber(sel.QuotaLimit), sel.CreditLimit,
		)
		detailBox := StyleCardHighlight.Width(m.cardWidth(68)).Render(detailContent)
		b.WriteString("\n" + detailBox + "\n")
	}

	return b.String()
}

func (m Model) renderExportImport() string {
	var b strings.Builder

	if m.actionMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render(m.actionMsg) + "\n\n")
	}

	// 1. Import from Path or URL Dialog
	if m.exportImportAction == 1 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("── 📥 IMPORT PROVIDERS DARI URL / FILE ──") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("Paste URL backup (http/https) atau path file lokal:") + "\n")
		b.WriteString(m.importPathInput.View() + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to download & import | Esc to cancel]") + "\n")
		return b.String()
	}

	// 2. Paste JSON Dialog
	if m.exportImportAction == 2 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("── 📋 PASTE RAW PROVIDERS JSON ──") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("Paste providers JSON array / backup:") + "\n")
		b.WriteString(m.importJSONInput.View() + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("[Press Enter to parse & import | Esc to cancel]") + "\n")
		return b.String()
	}

	// 3. Export Confirmation Dialog
	if m.exportImportAction == 3 {
		connCount := 0
		for _, p := range m.providers {
			connCount += p.ConnectedCount
		}
		confirmBox := StyleCardHighlight.Width(m.cardWidth(68)).Render(
			fmt.Sprintf("⚠️  EXPORT CONFIRMATION\n\n"+
				"Are you sure you want to export all provider connections?\n"+
				"• Total Connections: %d linked accounts\n"+
				"• Destination:       New timestamped folder in ~/.lam-router/exports/<timestamp>/\n\n"+
				"[Press 'y' / Enter to Confirm Export  |  'n' / Esc to Cancel]",
				connCount),
		)
		b.WriteString(confirmBox + "\n")
		return b.String()
	}

	// 4. Normal View
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render("📦 PROVIDERS BACKUP & RESTORE UTILITY") + "\n\n")

	connCount := 0
	for _, p := range m.providers {
		connCount += p.ConnectedCount
	}

	folderDesc := "Auto-created under ~/.lam-router/exports/<timestamp>/"
	if m.latestExportFolder != "" {
		folderDesc = m.latestExportFolder
	}

	statusCard := StyleCard.Width(min(70, m.width-12)).Render(
		fmt.Sprintf("• Active Upstream Drivers:   %d Supported\n"+
			"• Linked Accounts / Sesi:    %d Connected in Pool\n"+
			"• Export Target Location:    %s\n"+
			"• Scope:                      Provider configurations & API keys only",
			len(m.providers), connCount, folderDesc),
	)
	b.WriteString(statusCard + "\n\n")

	actionsCard := StyleCardHighlight.Width(min(70, m.width-12)).Render(
		"🚀 QUICK ACTIONS:\n\n" +
			"• [x] : Export providers (otomatis buat folder baru di ~/.lam-router/exports/)\n" +
			"• [i] : Import providers dari URL (http/https) atau path file lokal\n" +
			"• [p] : Paste snippet raw JSON langsung\n" +
			"• [r] : Refresh provider connection data dari server",
	)
	b.WriteString(actionsCard + "\n\n")

	if m.latestExportFolder != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("📁 FOLDER HASIL EXPORT TERBARU:") + "\n")
		folderCard := StyleCard.Width(min(70, m.width-12)).Render(
			fmt.Sprintf("Path: %s\n├── providers-backup.json (siap dipakai / di-share)\n└── summary.txt", m.latestExportFolder),
		)
		b.WriteString(folderCard + "\n\n")
	}

	if m.exportedJSONContent != "" {
		preview := m.exportedJSONContent
		if len(preview) > 600 {
			preview = preview[:600] + "\n... (truncated preview)"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("📄 Latest Exported JSON Preview:") + "\n")
		b.WriteString(StyleCard.Width(min(70, m.width-12)).Render(preview) + "\n")
	}

	return b.String()
}

func (m Model) renderLogs() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(
		fmt.Sprintf("📜 RECENT GATEWAY REQUEST LOGS (%d Recorded) — [Enter] View Detail  [r] Refresh:", len(m.logs))) + "\n\n")

	if len(m.logs) == 0 {
		b.WriteString(StyleMutedItem.Render("No request logs recorded yet. Send a prompt via Playground to generate logs!\n"))
		return b.String()
	}

	maxShow := m.visibleRows(11)
	start := max(0, min(m.logCursor-maxShow/2, len(m.logs)-maxShow))
	end := min(start+maxShow, len(m.logs))

	// Responsive Table Header & Columns
	isNarrow := m.width > 0 && m.width < 85
	modelW := 28
	if m.width >= 115 {
		modelW = 38
	} else if isNarrow {
		modelW = 20
	}

	if isNarrow {
		th := fmt.Sprintf("  %-8s %-7s %-7s %-*s %s", "TIME", "STATUS", "LAT", modelW, "TARGET MODEL", "TOKENS")
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Render(th) + "\n")
	} else {
		th := fmt.Sprintf("  %-8s %-8s %-8s %-*s %-10s %-8s %s", "TIME", "STATUS", "LATENCY", modelW, "TARGET MODEL", "TOKENS", "COST", "ROUTING")
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Render(th) + "\n")
	}

	for i := start; i < end; i++ {
		l := m.logs[i]
		cursor := "  "
		style := StyleNormalItem
		if i == m.logCursor {
			cursor = "❯ "
			style = StyleSelectedItem
		}

		timeStr := time.UnixMilli(l.CreatedAt).Format("15:04:05")

		statusPill := lipgloss.NewStyle().Foreground(ColorSuccess).Render("200 OK")
		if l.StatusCode >= 400 && l.StatusCode < 500 {
			statusPill = lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("%d ERR", l.StatusCode))
		} else if l.StatusCode >= 500 {
			statusPill = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(fmt.Sprintf("%d ERR", l.StatusCode))
		}

		latStr := fmt.Sprintf("%dms", l.LatencyMs)
		tokStr := fmt.Sprintf("%d tok", l.TotalTokens)
		costStr := fmt.Sprintf("$%.4f", l.EstimatedCost)

		routeStr := lipgloss.NewStyle().Foreground(ColorSuccess).Render("[DIRECT]")
		if l.FallbackOccurred {
			routeStr = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("[CASCADE]")
		}

		modelStr := truncate(l.Model, modelW)

		var line string
		if isNarrow {
			line = fmt.Sprintf("%s%-8s %-7s %-7s %-*s %s",
				cursor, timeStr, statusPill, latStr, modelW, modelStr, tokStr)
		} else {
			line = fmt.Sprintf("%s%-8s %-8s %-8s %-*s %-10s %-8s %s",
				cursor, timeStr, statusPill, latStr, modelW, modelStr, tokStr, costStr, routeStr)
		}
		b.WriteString(style.Render(line) + "\n")
	}

	if m.logDetail && len(m.logs) > 0 && m.logCursor < len(m.logs) {
		sel := m.logs[m.logCursor]
		timeFull := time.UnixMilli(sel.CreatedAt).Format("2006-01-02 15:04:05")
		detailContent := fmt.Sprintf(
			"Request ID:       %s\n"+
				"Timestamp:        %s\n"+
				"Target Model:     %s\n"+
				"Upstream Driver:  %s (API Key: %s)\n"+
				"HTTP Status:      %d\n"+
				"Latency:          %d ms\n"+
				"Tokens Breakdown: Prompt: %d | Comp: %d | Total: %d (Cached: %d, Reasoning: %d)\n"+
				"Estimated Cost:   $%.6f\n"+
				"Failover Cascade: %v\n\n"+
				"[Press Enter or Esc to dismiss detail card]",
			sel.ID, timeFull, sel.Model, sel.ProviderID, sel.APIKeyID,
			sel.StatusCode, sel.LatencyMs,
			sel.PromptTokens, sel.CompletionTokens, sel.TotalTokens, sel.CachedTokens, sel.ReasoningTokens,
			sel.EstimatedCost, sel.FallbackOccurred,
		)
		detailBox := StyleCardHighlight.Width(m.cardWidth(72)).Render(detailContent)
		b.WriteString("\n" + detailBox + "\n")
	}

	return b.String()
}

func (m Model) renderFooter() string {
	var keys []string

	if m.activeTab == tabProviders {
		if m.provAction == provActionAddConn {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Save API Key")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.provAction == provActionDeleteConfirm {
			k1 := StyleHelpKey.Render("y") + StyleHelpDesc.Render(" Confirm Disconnect")
			k2 := StyleHelpKey.Render("n/Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else {
			keys = append(keys, StyleHelpKey.Render("Tab/1-4")+StyleHelpDesc.Render(" Tabs"))
			keys = append(keys, StyleHelpKey.Render("↑/↓")+StyleHelpDesc.Render(" Select"))
			keys = append(keys, StyleHelpKey.Render("←/→/f")+StyleHelpDesc.Render(" Filter"))
			keys = append(keys, StyleHelpKey.Render("Enter")+StyleHelpDesc.Render(" Dropdown"))
			keys = append(keys, StyleHelpKey.Render("a")+StyleHelpDesc.Render(" Add Con"))
			keys = append(keys, StyleHelpKey.Render("d")+StyleHelpDesc.Render(" Dis Con"))
			keys = append(keys, StyleHelpKey.Render("t")+StyleHelpDesc.Render(" Test Con"))
			keys = append(keys, StyleHelpKey.Render("T")+StyleHelpDesc.Render(" Test All"))
			keys = append(keys, StyleHelpKey.Render("q")+StyleHelpDesc.Render(" Quit"))
			return lipgloss.JoinHorizontal(lipgloss.Center, keys...)
		}
	}

	if m.activeTab == tabCombos {
		if m.comboAction == comboActionCreate {
			if m.comboCreateStep == 0 {
				k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Next")
				k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
				return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
			}
			k1 := StyleHelpKey.Render("Space") + StyleHelpDesc.Render(" Toggle Select (#priority)")
			k2 := StyleHelpKey.Render("↑/↓") + StyleHelpDesc.Render(" Navigate")
			k3 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Create Combo")
			k4 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2, k3, k4)
		} else if m.comboAction == comboActionEdit {
			k1 := StyleHelpKey.Render("Space") + StyleHelpDesc.Render(" Toggle Select (#priority)")
			k2 := StyleHelpKey.Render("↑/↓") + StyleHelpDesc.Render(" Navigate")
			k3 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Save Changes")
			k4 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2, k3, k4)
		} else if m.comboAction == comboActionDeleteConfirm {
			k1 := StyleHelpKey.Render("y") + StyleHelpDesc.Render(" Confirm Delete")
			k2 := StyleHelpKey.Render("n/Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else {
			keys = append(keys, StyleHelpKey.Render("Tab/1-4")+StyleHelpDesc.Render(" Tabs"))
			keys = append(keys, StyleHelpKey.Render("↑/↓")+StyleHelpDesc.Render(" Select"))
			keys = append(keys, StyleHelpKey.Render("Enter")+StyleHelpDesc.Render(" Expand"))
			keys = append(keys, StyleHelpKey.Render("c")+StyleHelpDesc.Render(" Create"))
			keys = append(keys, StyleHelpKey.Render("e")+StyleHelpDesc.Render(" Edit"))
			keys = append(keys, StyleHelpKey.Render("d")+StyleHelpDesc.Render(" Delete"))
			keys = append(keys, StyleHelpKey.Render("t")+StyleHelpDesc.Render(" Toggle"))
			keys = append(keys, StyleHelpKey.Render("p")+StyleHelpDesc.Render(" Test"))
			keys = append(keys, StyleHelpKey.Render("q")+StyleHelpDesc.Render(" Quit"))
			return lipgloss.JoinHorizontal(lipgloss.Center, keys...)
		}
	}

	if m.activeTab == tabKeys {
		if m.keyAction == keyActionCreate {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Next/Create")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.keyAction == keyActionSetLimits {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Next/Save Limits")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.keyAction == keyActionEdit {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Save Name")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.keyAction == keyActionDeleteConfirm {
			k1 := StyleHelpKey.Render("y") + StyleHelpDesc.Render(" Confirm Revoke")
			k2 := StyleHelpKey.Render("n/Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else {
			keys = append(keys, StyleHelpKey.Render("Tab/1-6")+StyleHelpDesc.Render(" Tabs"))
			keys = append(keys, StyleHelpKey.Render("↑/↓")+StyleHelpDesc.Render(" Select"))
			keys = append(keys, StyleHelpKey.Render("Enter")+StyleHelpDesc.Render(" Details"))
			keys = append(keys, StyleHelpKey.Render("c")+StyleHelpDesc.Render(" Create"))
			keys = append(keys, StyleHelpKey.Render("e")+StyleHelpDesc.Render(" Edit Name"))
			keys = append(keys, StyleHelpKey.Render("l")+StyleHelpDesc.Render(" Set Limits"))
			keys = append(keys, StyleHelpKey.Render("d")+StyleHelpDesc.Render(" Revoke"))
			keys = append(keys, StyleHelpKey.Render("t")+StyleHelpDesc.Render(" Toggle"))
			keys = append(keys, StyleHelpKey.Render("q")+StyleHelpDesc.Render(" Quit"))
			return lipgloss.JoinHorizontal(lipgloss.Center, keys...)
		}
	}

	if m.activeTab == tabExportImport {
		if m.exportImportAction == 1 {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Download & Import")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.exportImportAction == 2 {
			k1 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Parse & Import")
			k2 := StyleHelpKey.Render("Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		} else if m.exportImportAction == 3 {
			k1 := StyleHelpKey.Render("y/Enter") + StyleHelpDesc.Render(" Confirm Export")
			k2 := StyleHelpKey.Render("n/Esc") + StyleHelpDesc.Render(" Cancel")
			return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2)
		}
		keys = append(keys, StyleHelpKey.Render("Tab/1-6")+StyleHelpDesc.Render(" Tabs"))
		keys = append(keys, StyleHelpKey.Render("e/x")+StyleHelpDesc.Render(" Export"))
		keys = append(keys, StyleHelpKey.Render("i")+StyleHelpDesc.Render(" Import URL/File"))
		keys = append(keys, StyleHelpKey.Render("p")+StyleHelpDesc.Render(" Paste JSON"))
		keys = append(keys, StyleHelpKey.Render("r")+StyleHelpDesc.Render(" Refresh"))
		keys = append(keys, StyleHelpKey.Render("q")+StyleHelpDesc.Render(" Quit"))
		return lipgloss.JoinHorizontal(lipgloss.Center, keys...)
	}

	if m.activeTab == tabLogs {
		keys = append(keys, StyleHelpKey.Render("Tab/1-6")+StyleHelpDesc.Render(" Tabs"))
		keys = append(keys, StyleHelpKey.Render("↑/↓")+StyleHelpDesc.Render(" Navigate"))
		keys = append(keys, StyleHelpKey.Render("Enter")+StyleHelpDesc.Render(" Detail"))
		keys = append(keys, StyleHelpKey.Render("r")+StyleHelpDesc.Render(" Refresh"))
		keys = append(keys, StyleHelpKey.Render("q")+StyleHelpDesc.Render(" Quit"))
		return lipgloss.JoinHorizontal(lipgloss.Center, keys...)
	}

	k1 := StyleHelpKey.Render("Tab/1-6") + StyleHelpDesc.Render(" Switch Tab")
	k2 := StyleHelpKey.Render("↑/↓ / j/k") + StyleHelpDesc.Render(" Navigate")
	k3 := StyleHelpKey.Render("Enter") + StyleHelpDesc.Render(" Select/Run")
	k4 := StyleHelpKey.Render("←/→ / m") + StyleHelpDesc.Render(" Switch Model")
	k5 := StyleHelpKey.Render("r") + StyleHelpDesc.Render(" Refresh")
	k6 := StyleHelpKey.Render("q") + StyleHelpDesc.Render(" Quit")

	return lipgloss.JoinHorizontal(lipgloss.Center, k1, k2, k3, k4, k5, k6)
}

func Run(baseURL string) error {
	p := tea.NewProgram(NewModel(baseURL), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func formatNumber(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000.0)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000.0)
	}
	return fmt.Sprintf("%d", n)
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
