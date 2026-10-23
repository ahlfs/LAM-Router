package db

import (
	"database/sql"
	"os"
	"testing"

	"lamrouter/internal/handlerutil"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpFile, err := os.CreateTemp("", "test_repos_*.sqlite")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := OpenDatabase(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("OpenDatabase failed: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	// Create tables according to the actual schema
	schema := []string{
		`CREATE TABLE apiKeys (
			id TEXT PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT,
			machineId TEXT,
			isActive INTEGER DEFAULT 1,
			createdAt TEXT NOT NULL
		);`,
		`CREATE TABLE providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			authType TEXT NOT NULL,
			name TEXT,
			email TEXT,
			priority INTEGER,
			isActive INTEGER DEFAULT 1,
			data TEXT NOT NULL,
			lastUsedAt TEXT,
			consecutiveUseCount INTEGER DEFAULT 0,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);`,
		`CREATE TABLE combos (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			kind TEXT,
			models TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);`,
		`CREATE TABLE kv (
			scope TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			PRIMARY KEY (scope, key)
		);`,
		`CREATE TABLE providerNodes (
			id TEXT PRIMARY KEY,
			type TEXT,
			name TEXT,
			data TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			rate_limit INTEGER DEFAULT 0,
			quota_limit INTEGER DEFAULT 0,
			usage_tokens INTEGER DEFAULT 0,
			credit_limit REAL DEFAULT 0,
			usage_cost REAL DEFAULT 0,
			allowed_models TEXT,
			last_used_at INTEGER DEFAULT 0,
			last_model TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS api_key_model_usage (
			key_id TEXT NOT NULL,
			model TEXT NOT NULL,
			requests INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			cost REAL NOT NULL DEFAULT 0,
			first_used INTEGER NOT NULL DEFAULT 0,
			last_used INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (key_id, model)
		);`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id TEXT PRIMARY KEY,
			api_key_id TEXT,
			api_key_name TEXT,
			provider_id TEXT NOT NULL,
			model TEXT NOT NULL,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			estimated_cost REAL DEFAULT 0
		);`,
	}

	for _, query := range schema {
		if _, err := db.Exec(query); err != nil {
			cleanup()
			t.Fatalf("failed to create table: %v", err)
		}
	}

	return db, cleanup
}

func TestIncrementApiKeyUsage(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO api_keys (id, key, name, enabled, quota_limit, usage_tokens, credit_limit, usage_cost, created_at) VALUES
		('k-1', 'lam-live-test', 'Test Key', 1, 10000, 100, 5.0, 0.05, 1720000000000);`)
	if err != nil {
		t.Fatalf("failed to seed api_keys: %v", err)
	}

	repo := NewRepo(db)

	// Increment usage by 250 tokens and 0.02 cost, attributed to one model
	if err := repo.IncrementApiKeyUsage("lam-live-test", 250, 0.02, "gemini-3.8-flash-high"); err != nil {
		t.Fatalf("IncrementApiKeyUsage failed: %v", err)
	}

	// Verify updated values
	apiKey, err := repo.GetApiKeyByKey("lam-live-test")
	if err != nil {
		t.Fatalf("GetApiKeyByKey failed: %v", err)
	}
	if apiKey == nil {
		t.Fatal("expected apiKey to be found")
	}
	if apiKey.UsageTokens != 350 {
		t.Errorf("expected UsageTokens=350, got %d", apiKey.UsageTokens)
	}
	if apiKey.UsageCost < 0.069 || apiKey.UsageCost > 0.071 {
		t.Errorf("expected UsageCost ~0.07, got %f", apiKey.UsageCost)
	}

	// The rollup row and the last-used stamp are written in the same transaction.
	var model string
	var tokens, requests int64
	if err := db.QueryRow(`SELECT model, total_tokens, requests FROM api_key_model_usage WHERE key_id = 'k-1'`).
		Scan(&model, &tokens, &requests); err != nil {
		t.Fatalf("expected rollup row: %v", err)
	}
	if model != "gemini-3.8-flash-high" || tokens != 250 || requests != 1 {
		t.Errorf("unexpected rollup row: model=%q tokens=%d requests=%d", model, tokens, requests)
	}

	var lastModel string
	var lastUsed int64
	if err := db.QueryRow(`SELECT COALESCE(last_model, ''), COALESCE(last_used_at, 0) FROM api_keys WHERE id = 'k-1'`).
		Scan(&lastModel, &lastUsed); err != nil {
		t.Fatalf("read last-used stamps: %v", err)
	}
	if lastModel != "gemini-3.8-flash-high" || lastUsed == 0 {
		t.Errorf("expected last model/at to be stamped, got %q / %d", lastModel, lastUsed)
	}
}

func TestIncrementApiKeyUsageAccumulatesPerModel(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES
		('k-2', 'lam-live-two', 'Two', 1, 1720000000000);`)
	if err != nil {
		t.Fatalf("seed api_keys: %v", err)
	}
	repo := NewRepo(db)

	if err := repo.IncrementApiKeyUsage("lam-live-two", 100, 0.01, "model-a"); err != nil {
		t.Fatalf("first increment: %v", err)
	}
	if err := repo.IncrementApiKeyUsage("lam-live-two", 250, 0.02, "model-a"); err != nil {
		t.Fatalf("second increment: %v", err)
	}
	if err := repo.IncrementApiKeyUsage("lam-live-two", 40, 0.5, "model-b"); err != nil {
		t.Fatalf("third increment: %v", err)
	}
	// Non-model values must not create rollup rows.
	if err := repo.IncrementApiKeyUsage("lam-live-two", 10, 0, "auth"); err != nil {
		t.Fatalf("auth increment: %v", err)
	}

	rows, err := repo.KeyUsageSummary(5)
	if err != nil {
		t.Fatalf("KeyUsageSummary: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 key, got %d", len(rows))
	}
	k := rows[0]
	if len(k.Models) != 2 {
		t.Fatalf("expected 2 models (auth excluded), got %d: %+v", len(k.Models), k.Models)
	}
	if k.Models[0].Model != "model-a" || k.Models[0].TotalTokens != 350 || k.Models[0].Requests != 2 {
		t.Errorf("unexpected top model: %+v", k.Models[0])
	}
	if k.TrackedTokens != 390 {
		t.Errorf("expected tracked tokens 390, got %d", k.TrackedTokens)
	}
	if k.UsageTokens != 400 {
		t.Errorf("expected lifetime counter 400 (auth tokens still billed), got %d", k.UsageTokens)
	}
	if k.LastModel != "model-b" {
		t.Errorf("expected last model model-b, got %q", k.LastModel)
	}
}

func TestBackfillKeyModelUsageIsIdempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES
		('k-3', 'lam-live-three', 'Three', 1, 1720000000000);`)
	if err != nil {
		t.Fatalf("seed api_keys: %v", err)
	}
	for i, row := range []struct {
		id      string
		keyID   string
		model   string
		tokens  int64
		status  int
		created int64
	}{
		{"r1", "k-3", "model-x", 100, 200, 1000},
		{"r2", "k-3", "model-x", 200, 200, 2000},
		{"r3", "k-3", "model-y", 50, 200, 3000},
		{"r4", "k-3", "auth", 0, 401, 4000},            // must be skipped
		{"r5", "other-key", "model-x", 999, 200, 5000}, // unknown key: skipped
	} {
		if _, err := db.Exec(`INSERT INTO request_logs (id, api_key_id, provider_id, model, total_tokens, status_code, latency_ms, created_at)
			VALUES (?, ?, 'gemini', ?, ?, ?, 10, ?)`, row.id, row.keyID, row.model, row.tokens, row.status, row.created); err != nil {
			t.Fatalf("seed request_logs row %d: %v", i, err)
		}
	}

	repo := NewRepo(db)
	affected, err := repo.BackfillKeyModelUsage()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if affected != 2 {
		t.Errorf("expected 2 rollup rows, got %d", affected)
	}

	var total int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(total_tokens),0) FROM api_key_model_usage WHERE key_id = 'k-3'`).Scan(&total); err != nil {
		t.Fatalf("sum rollup: %v", err)
	}
	if total != 350 {
		t.Errorf("expected 350 tokens from logs, got %d", total)
	}

	// Second call must not double count.
	affected, err = repo.BackfillKeyModelUsage()
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected no-op on second backfill, got %d rows", affected)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(total_tokens),0) FROM api_key_model_usage`).Scan(&total); err != nil {
		t.Fatalf("sum rollup after second call: %v", err)
	}
	if total != 350 {
		t.Errorf("expected still 350 tokens, got %d", total)
	}

	var lastModel string
	if err := db.QueryRow(`SELECT COALESCE(last_model,'') FROM api_keys WHERE id = 'k-3'`).Scan(&lastModel); err != nil {
		t.Fatalf("read last_model: %v", err)
	}
	if lastModel != "model-y" {
		t.Errorf("expected last model model-y (latest log), got %q", lastModel)
	}
}

func TestRecountApiKeyUsage(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO api_keys (id, key, name, enabled, usage_tokens, usage_cost, created_at) VALUES
		('k-5', 'lam-live-five', 'Five', 1, 999999, 42.5, 1720000000000);`)
	if err != nil {
		t.Fatalf("seed api_keys: %v", err)
	}
	repo := NewRepo(db)
	if err := repo.IncrementApiKeyUsage("lam-live-five", 120, 0.12, "model-r"); err != nil {
		t.Fatalf("increment: %v", err)
	}

	// Corrupt the counters the way a pre-rollup key looks, then recount.
	if _, err := db.Exec(`UPDATE api_keys SET usage_tokens = 1000, usage_cost = 1.0 WHERE id = 'k-5'`); err != nil {
		t.Fatalf("corrupt counters: %v", err)
	}

	res, err := repo.RecountApiKeyUsage("k-5")
	if err != nil {
		t.Fatalf("recount: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].TokensBefore != 1000 || res[0].TokensAfter != 120 {
		t.Errorf("unexpected tokens before/after: %d -> %d", res[0].TokensBefore, res[0].TokensAfter)
	}
	if res[0].CostBefore != 1.0 || res[0].CostAfter < 0.119 || res[0].CostAfter > 0.121 {
		t.Errorf("unexpected cost before/after: %f -> %f", res[0].CostBefore, res[0].CostAfter)
	}
	if res[0].Models != 1 {
		t.Errorf("expected 1 model, got %d", res[0].Models)
	}

	var tokens int64
	if err := db.QueryRow(`SELECT usage_tokens FROM api_keys WHERE id = 'k-5'`).Scan(&tokens); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if tokens != 120 {
		t.Errorf("expected counter rebuilt to 120, got %d", tokens)
	}

	// Recounting every key is the empty-ID form.
	all, err := repo.RecountApiKeyUsage("")
	if err != nil {
		t.Fatalf("recount all: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected recount-all to return 1 key, got %d", len(all))
	}
}

func TestResetKeyModelUsage(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO api_keys (id, key, name, enabled, created_at) VALUES
		('k-4', 'lam-live-four', 'Four', 1, 1720000000000);`)
	if err != nil {
		t.Fatalf("seed api_keys: %v", err)
	}
	repo := NewRepo(db)
	if err := repo.IncrementApiKeyUsage("lam-live-four", 10, 0.001, "model-z"); err != nil {
		t.Fatalf("increment: %v", err)
	}
	if err := repo.ResetKeyModelUsage("k-4"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	rows, err := repo.KeyUsageSummary(5)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Models) != 0 || rows[0].TrackedTokens != 0 {
		t.Errorf("expected empty rollup after reset, got %+v", rows)
	}
}

func TestValidateApiKey(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed data
	_, err := db.Exec(`INSERT INTO apiKeys (id, key, name, machineId, isActive, createdAt) VALUES
		('1', 'valid-key', 'Test Key 1', 'mac-1', 1, '2026-07-18T00:00:00Z'),
		('2', 'inactive-key', 'Test Key 2', 'mac-2', 0, '2026-07-18T00:00:00Z');`)
	if err != nil {
		t.Fatalf("failed to seed apiKeys: %v", err)
	}

	repo := NewRepo(db)

	// Test valid key
	valid, err := repo.ValidateApiKey("valid-key")
	if err != nil {
		t.Errorf("ValidateApiKey returned error: %v", err)
	}
	if !valid {
		t.Error("expected valid-key to be valid")
	}

	// Test inactive key
	inactive, err := repo.ValidateApiKey("inactive-key")
	if err != nil {
		t.Errorf("ValidateApiKey returned error: %v", err)
	}
	if inactive {
		t.Error("expected inactive-key to be invalid")
	}

	// Test non-existent key
	nonexistent, err := repo.ValidateApiKey("nonexistent-key")
	if err != nil {
		t.Errorf("ValidateApiKey returned error: %v", err)
	}
	if nonexistent {
		t.Error("expected nonexistent-key to be invalid")
	}

	// Test GetApiKeyByKey
	keyObj, err := repo.GetApiKeyByKey("valid-key")
	if err != nil {
		t.Errorf("GetApiKeyByKey returned error: %v", err)
	}
	if keyObj == nil {
		t.Fatal("expected key details to be returned")
	}
	if keyObj.ID != "1" || keyObj.Key != "valid-key" || *keyObj.Name != "Test Key 1" || keyObj.IsActive != 1 {
		t.Errorf("unexpected APIKey fields: %+v", keyObj)
	}
}

func TestGetProviderConnections(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed data
	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt) VALUES
		('1', 'openai', 'apikey', 'OpenAI 1', 'openai1@test.com', 2, 1, '{"apiKey": "op1"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'),
		('2', 'openai', 'apikey', 'OpenAI 2', 'openai2@test.com', 1, 1, '{"apiKey": "op2"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'),
		('3', 'openai', 'apikey', 'OpenAI Inactive', 'openai3@test.com', 3, 0, '{"apiKey": "op3"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'),
		('4', 'anthropic', 'apikey', 'Anthropic 1', 'anthropic@test.com', NULL, 1, '{"apiKey": "ant"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z');`)
	if err != nil {
		t.Fatalf("failed to seed providerConnections: %v", err)
	}

	repo := NewRepo(db)

	// Get all OpenAI active connections (should be sorted by priority ASC)
	conns, err := repo.GetProviderConnections("openai", true)
	if err != nil {
		t.Errorf("GetProviderConnections failed: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("expected 2 active connections, got %d", len(conns))
	}
	// Priority 1 should be first, then priority 2
	if conns[0].ID != "2" || conns[1].ID != "1" {
		t.Errorf("expected sorted connections: conns[0].ID=%s (expected 2), conns[1].ID=%s (expected 1)", conns[0].ID, conns[1].ID)
	}

	// Get all OpenAI connections (including inactive)
	allConns, err := repo.GetProviderConnections("openai", false)
	if err != nil {
		t.Errorf("GetProviderConnections failed: %v", err)
	}
	if len(allConns) != 3 {
		t.Fatalf("expected 3 connections, got %d", len(allConns))
	}

	// Get Anthropic connections (testing priority NULL -> 999999 sorting)
	antConns, err := repo.GetProviderConnections("anthropic", true)
	if err != nil {
		t.Errorf("GetProviderConnections failed: %v", err)
	}
	if len(antConns) != 1 {
		t.Fatalf("expected 1 anthropic connection, got %d", len(antConns))
	}
	if antConns[0].Priority != nil {
		t.Errorf("expected null priority, got %d", *antConns[0].Priority)
	}

	// Get all active connections across all providers
	allActive, err := repo.GetProviderConnections("", true)
	if err != nil {
		t.Errorf("GetProviderConnections failed: %v", err)
	}
	if len(allActive) != 3 {
		t.Errorf("expected 3 active connections total, got %d", len(allActive))
	}

	// Get all connections across all providers (active and inactive)
	allTotal, err := repo.GetProviderConnections("", false)
	if err != nil {
		t.Errorf("GetProviderConnections failed: %v", err)
	}
	if len(allTotal) != 4 {
		t.Errorf("expected 4 connections total, got %d", len(allTotal))
	}
}

func TestModelAliases(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed data (simulating JSON strings and raw strings)
	_, err := db.Exec(`INSERT INTO kv (scope, key, value) VALUES
		('modelAliases', 'gpt-4', '"gpt-4o"'),
		('modelAliases', 'claude-3', '"claude-3-5-sonnet"'),
		('modelAliases', 'raw-model', 'gpt-4-raw'),
		('otherScope', 'gpt-4', '"other-val"');`)
	if err != nil {
		t.Fatalf("failed to seed kv: %v", err)
	}

	repo := NewRepo(db)

	// Test GetModelAlias
	val, err := repo.GetModelAlias("gpt-4")
	if err != nil {
		t.Errorf("GetModelAlias failed: %v", err)
	}
	if val != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", val)
	}

	// Test GetModelAlias with raw string (non-JSON string value)
	val, err = repo.GetModelAlias("raw-model")
	if err != nil {
		t.Errorf("GetModelAlias failed: %v", err)
	}
	if val != "gpt-4-raw" {
		t.Errorf("expected gpt-4-raw, got %s", val)
	}

	// Test nonexistent alias
	val, err = repo.GetModelAlias("nonexistent")
	if err != nil {
		t.Errorf("expected nil err for nonexistent alias, got: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for nonexistent alias, got: %s", val)
	}

	// Test GetModelAliases (all aliases)
	aliases, err := repo.GetModelAliases()
	if err != nil {
		t.Errorf("GetModelAliases failed: %v", err)
	}
	if len(aliases) != 3 {
		t.Errorf("expected 3 aliases, got %d", len(aliases))
	}
	if aliases["gpt-4"] != "gpt-4o" || aliases["claude-3"] != "claude-3-5-sonnet" || aliases["raw-model"] != "gpt-4-raw" {
		t.Errorf("unexpected aliases mapping: %+v", aliases)
	}
}

func TestCombos(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed data
	_, err := db.Exec(`INSERT INTO combos (id, name, kind, models, createdAt, updatedAt) VALUES
		('c1', 'combo-1', 'chat', '[{"model":"gpt-4o","weight":1}]', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'),
		('c2', 'combo-2', NULL, '[]', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z');`)
	if err != nil {
		t.Fatalf("failed to seed combos: %v", err)
	}

	repo := NewRepo(db)

	// Test GetComboByName
	combo, err := repo.GetComboByName("combo-1")
	if err != nil {
		t.Errorf("GetComboByName failed: %v", err)
	}
	if combo == nil || combo.ID != "c1" || combo.Models != `[{"model":"gpt-4o","weight":1}]` {
		t.Errorf("unexpected combo by name: %+v", combo)
	}

	// Test GetComboById
	combo, err = repo.GetComboById("c2")
	if err != nil {
		t.Errorf("GetComboById failed: %v", err)
	}
	if combo == nil || combo.Name != "combo-2" || combo.Kind != nil {
		t.Errorf("unexpected combo by id: %+v", combo)
	}

	// Test GetCombos
	combos, err := repo.GetCombos()
	if err != nil {
		t.Errorf("GetCombos failed: %v", err)
	}
	if len(combos) != 2 {
		t.Errorf("expected 2 combos, got %d", len(combos))
	}
}

func TestGetProviderNodeByID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	nodeData := `{"prefix":"bn","apiType":"openai-compatible","baseUrl":"https://custom.example.com/v1/chat/completions"}`
	_, err := db.Exec(`INSERT INTO providerNodes (id, type, name, data, createdAt, updatedAt) VALUES
		('openai-compatible-chat-abc123', 'openai-compatible', 'Bun Node', ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		nodeData)
	if err != nil {
		t.Fatalf("failed to seed providerNodes: %v", err)
	}

	repo := NewRepo(db)

	// Existing node
	node, data, err := repo.GetProviderNodeByID("openai-compatible-chat-abc123")
	if err != nil {
		t.Fatalf("GetProviderNodeByID returned error: %v", err)
	}
	if node == nil {
		t.Fatal("expected node, got nil")
	}
	if node.ID != "openai-compatible-chat-abc123" {
		t.Errorf("expected ID 'openai-compatible-chat-abc123', got '%s'", node.ID)
	}
	if data == nil {
		t.Fatal("expected data, got nil")
	}
	if data.Prefix != "bn" {
		t.Errorf("expected prefix 'bn', got '%s'", data.Prefix)
	}
	if data.BaseURL != "https://custom.example.com/v1/chat/completions" {
		t.Errorf("expected baseUrl, got '%s'", data.BaseURL)
	}

	// Non-existent node
	node, data, err = repo.GetProviderNodeByID("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Errorf("expected nil for nonexistent node, got %+v", node)
	}
	if data != nil {
		t.Errorf("expected nil data for nonexistent node, got %+v", data)
	}
}

func TestGetProviderNodeByPrefix(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	node1Data := `{"prefix":"bn","apiType":"openai-compatible","baseUrl":"https://bn.example.com/v1/chat/completions"}`
	node2Data := `{"prefix":"cf","apiType":"openai-compatible","baseUrl":"https://cf.example.com/v1/chat/completions"}`
	_, err := db.Exec(`INSERT INTO providerNodes (id, type, name, data, createdAt, updatedAt) VALUES
		('node-bn', 'openai-compatible', 'Bun Node', ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'),
		('node-cf', 'openai-compatible', 'CF Node', ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		node1Data, node2Data)
	if err != nil {
		t.Fatalf("failed to seed providerNodes: %v", err)
	}

	repo := NewRepo(db)

	// Match by prefix "bn"
	node, data, err := repo.GetProviderNodeByPrefix("bn")
	if err != nil {
		t.Fatalf("GetProviderNodeByPrefix returned error: %v", err)
	}
	if node == nil {
		t.Fatal("expected node for prefix 'bn', got nil")
	}
	if node.ID != "node-bn" {
		t.Errorf("expected ID 'node-bn', got '%s'", node.ID)
	}
	if data.Prefix != "bn" {
		t.Errorf("expected prefix 'bn', got '%s'", data.Prefix)
	}

	// Match by prefix "cf"
	node, data, err = repo.GetProviderNodeByPrefix("cf")
	if err != nil {
		t.Fatalf("GetProviderNodeByPrefix returned error: %v", err)
	}
	if node == nil || node.ID != "node-cf" {
		t.Errorf("expected node-cf, got %+v", node)
	}

	// Non-existent prefix
	node, data, err = repo.GetProviderNodeByPrefix("zz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Errorf("expected nil for non-existent prefix, got %+v", node)
	}
}

func TestGetProviderConnectionByID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-abc', 'node-bn', 'apikey', 'Bun Connection', NULL, 1, 1, '{"apiKey":"sk-bn"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`)
	if err != nil {
		t.Fatalf("failed to seed providerConnections: %v", err)
	}

	repo := NewRepo(db)

	// Existing connection
	conn, err := repo.GetProviderConnectionByID("conn-abc")
	if err != nil {
		t.Fatalf("GetProviderConnectionByID returned error: %v", err)
	}
	if conn == nil {
		t.Fatal("expected connection, got nil")
	}
	if conn.ID != "conn-abc" {
		t.Errorf("expected ID 'conn-abc', got '%s'", conn.ID)
	}
	if conn.Provider != "node-bn" {
		t.Errorf("expected provider 'node-bn', got '%s'", conn.Provider)
	}

	// Non-existent connection
	conn, err = repo.GetProviderConnectionByID("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn != nil {
		t.Errorf("expected nil for nonexistent connection, got %+v", conn)
	}
}

func TestGetString(t *testing.T) {
	m := map[string]any{
		"name":     "pool-1",
		"strategy": "round-robin",
		"count":    3,
		"active":   true,
	}

	if got := handlerutil.GetString(m, "name"); got != "pool-1" {
		t.Errorf("expected 'pool-1', got '%s'", got)
	}
	if got := handlerutil.GetString(m, "strategy"); got != "round-robin" {
		t.Errorf("expected 'round-robin', got '%s'", got)
	}
	if got := handlerutil.GetString(m, "missing"); got != "" {
		t.Errorf("expected '' for missing key, got '%s'", got)
	}
	if got := handlerutil.GetString(m, "count"); got != "" {
		t.Errorf("expected '' for non-string value, got '%s'", got)
	}
	if got := handlerutil.GetString(m, "active"); got != "" {
		t.Errorf("expected '' for bool value, got '%s'", got)
	}
}

func TestNextURL_Empty(t *testing.T) {
	pool := &ProxyPool{URLs: []string{}}
	if got := pool.NextURL(); got != "" {
		t.Errorf("expected '' for empty URLs, got '%s'", got)
	}
}

func TestNextURL_SingleURL(t *testing.T) {
	pool := &ProxyPool{URLs: []string{"http://proxy1:8080"}}
	for i := 0; i < 3; i++ {
		if got := pool.NextURL(); got != "http://proxy1:8080" {
			t.Errorf("iteration %d: expected 'http://proxy1:8080', got '%s'", i, got)
		}
	}
}

func TestNextURL_RoundRobin(t *testing.T) {
	pool := &ProxyPool{URLs: []string{"http://a:8080", "http://b:8080", "http://c:8080"}}

	// atomic counter starts at 0, first AddUint64 returns 1: 1%3=1 -> b, 2%3=2 -> c, 3%3=0 -> a, 4%3=1 -> b
	expected := []string{
		"http://b:8080",
		"http://c:8080",
		"http://a:8080",
		"http://b:8080",
	}
	for i, want := range expected {
		if got := pool.NextURL(); got != want {
			t.Errorf("iteration %d: expected '%s', got '%s'", i, want, got)
		}
	}
}

func TestGetProxyPool(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		isActive INTEGER DEFAULT 1
	);`)
	if err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO proxyPools (id, data, isActive) VALUES (?, ?, ?)`,
		"pool-1", `{"name":"Test Pool","strategy":"round-robin","urls":["http://p1:8080","http://p2:8080","http://p3:8080"]}`, 1,
	)
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}

	repo := NewRepo(db)
	pool, err := repo.GetProxyPool("pool-1")
	if err != nil {
		t.Fatalf("GetProxyPool failed: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	if pool.ID != "pool-1" {
		t.Errorf("expected ID 'pool-1', got '%s'", pool.ID)
	}
	if pool.Name != "Test Pool" {
		t.Errorf("expected Name 'Test Pool', got '%s'", pool.Name)
	}
	if !pool.IsActive {
		t.Error("expected IsActive true")
	}
	if pool.Strategy != "round-robin" {
		t.Errorf("expected Strategy 'round-robin', got '%s'", pool.Strategy)
	}
	if len(pool.URLs) != 3 {
		t.Fatalf("expected 3 URLs, got %d", len(pool.URLs))
	}
	if pool.URLs[0] != "http://p1:8080" || pool.URLs[1] != "http://p2:8080" || pool.URLs[2] != "http://p3:8080" {
		t.Errorf("unexpected URLs: %v", pool.URLs)
	}
}

func TestGetProxyPool_DefaultStrategy(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		isActive INTEGER DEFAULT 1
	);`)
	if err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	// No strategy field -> should default to "round-robin"
	_, err = db.Exec(`INSERT INTO proxyPools (id, data, isActive) VALUES (?, ?, ?)`,
		"pool-2", `{"name":"No Strategy Pool","urls":["http://p1:8080"]}`, 1,
	)
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}

	repo := NewRepo(db)
	pool, err := repo.GetProxyPool("pool-2")
	if err != nil {
		t.Fatalf("GetProxyPool failed: %v", err)
	}
	if pool.Strategy != "round-robin" {
		t.Errorf("expected default Strategy 'round-robin', got '%s'", pool.Strategy)
	}
}

func TestGetProxyPool_Inactive(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		isActive INTEGER DEFAULT 1
	);`)
	if err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO proxyPools (id, data, isActive) VALUES (?, ?, ?)`,
		"pool-3", `{"name":"Inactive Pool","urls":["http://p1:8080"]}`, 0,
	)
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}

	repo := NewRepo(db)
	pool, err := repo.GetProxyPool("pool-3")
	if err != nil {
		t.Fatalf("GetProxyPool failed: %v", err)
	}
	if pool.IsActive {
		t.Error("expected IsActive false for inactive pool")
	}
}

func TestGetProxyPool_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		isActive INTEGER DEFAULT 1
	);`)
	if err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	repo := NewRepo(db)
	pool, err := repo.GetProxyPool("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent pool")
	}
	if pool != nil {
		t.Errorf("expected nil pool, got %+v", pool)
	}
}

// --- usage tests ---

func TestGetUsageDaily_Exists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usageDaily (
		dateKey TEXT PRIMARY KEY,
		data TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatalf("failed to create usageDaily table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO usageDaily (dateKey, data) VALUES (?, ?)`,
		"2026-07-18", `{"requests":10,"tokens":5000}`,
	)
	if err != nil {
		t.Fatalf("failed to insert usage: %v", err)
	}

	repo := NewRepo(db)
	data, err := repo.GetUsageDaily("2026-07-18")
	if err != nil {
		t.Fatalf("GetUsageDaily failed: %v", err)
	}
	if data != `{"requests":10,"tokens":5000}` {
		t.Errorf("unexpected data: '%s'", data)
	}
}

func TestGetUsageDaily_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usageDaily (
		dateKey TEXT PRIMARY KEY,
		data TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatalf("failed to create usageDaily table: %v", err)
	}

	repo := NewRepo(db)
	_, err = repo.GetUsageDaily("2099-01-01")
	if err == nil {
		t.Error("expected error for non-existent dateKey")
	}
}

func TestInsertUsageHistory(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usageHistory (
		timestamp TEXT,
		provider TEXT,
		model TEXT,
		connectionId TEXT,
		apiKey TEXT,
		endpoint TEXT,
		promptTokens INTEGER,
		completionTokens INTEGER,
		cost REAL,
		status TEXT,
		tokens TEXT,
		meta TEXT
	);`)
	if err != nil {
		t.Fatalf("failed to create usageHistory table: %v", err)
	}

	repo := NewRepo(db)
	err = repo.InsertUsageHistory("openai", "gpt-4", "conn-1", "key-1", "/chat", 100, 50, 0.015, "success", 150, `{"reason":"test"}`, `{"response_time":200}`)
	if err != nil {
		t.Fatalf("InsertUsageHistory failed: %v", err)
	}

	// Verify by reading back
	var provider, model, status, tokens string
	var promptTokens int
	err = db.QueryRow(`SELECT provider, model, promptTokens, status, tokens FROM usageHistory WHERE connectionId = ?`, "conn-1").Scan(&provider, &model, &promptTokens, &status, &tokens)
	if err != nil {
		t.Fatalf("failed to read back usageHistory: %v", err)
	}
	if provider != "openai" || model != "gpt-4" || promptTokens != 100 || status != "success" {
		t.Errorf("unexpected row: provider=%s model=%s promptTokens=%d status=%s", provider, model, promptTokens, status)
	}
}

func TestUpsertUsageDaily_Insert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usageDaily (
		dateKey TEXT PRIMARY KEY,
		data TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatalf("failed to create usageDaily table: %v", err)
	}

	repo := NewRepo(db)
	err = repo.UpsertUsageDaily("2026-07-18", `{"requests":5}`)
	if err != nil {
		t.Fatalf("UpsertUsageDaily insert failed: %v", err)
	}

	data, err := repo.GetUsageDaily("2026-07-18")
	if err != nil {
		t.Fatalf("GetUsageDaily after insert failed: %v", err)
	}
	if data != `{"requests":5}` {
		t.Errorf("unexpected data: '%s'", data)
	}
}

func TestUpsertUsageDaily_Update(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usageDaily (
		dateKey TEXT PRIMARY KEY,
		data TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatalf("failed to create usageDaily table: %v", err)
	}

	repo := NewRepo(db)

	// Insert first
	err = repo.UpsertUsageDaily("2026-07-18", `{"requests":5}`)
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Replace
	err = repo.UpsertUsageDaily("2026-07-18", `{"requests":15,"tokens":1000}`)
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	data, err := repo.GetUsageDaily("2026-07-18")
	if err != nil {
		t.Fatalf("GetUsageDaily after update failed: %v", err)
	}
	if data != `{"requests":15,"tokens":1000}` {
		t.Errorf("expected replaced data, got '%s'", data)
	}
}

func TestInsertRequestDetail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS requestDetails (
		id TEXT PRIMARY KEY,
		timestamp TEXT,
		provider TEXT,
		model TEXT,
		connectionId TEXT,
		status TEXT,
		data TEXT
	);`)
	if err != nil {
		t.Fatalf("failed to create requestDetails table: %v", err)
	}

	repo := NewRepo(db)
	err = repo.InsertRequestDetail("req-1", "openai", "gpt-4", "conn-1", "success", `{"prompt":"hello"}`)
	if err != nil {
		t.Fatalf("InsertRequestDetail failed: %v", err)
	}

	// Verify
	var provider, model, status, data string
	err = db.QueryRow(`SELECT provider, model, status, data FROM requestDetails WHERE id = ?`, "req-1").Scan(&provider, &model, &status, &data)
	if err != nil {
		t.Fatalf("failed to read back requestDetail: %v", err)
	}
	if provider != "openai" || model != "gpt-4" || status != "success" || data != `{"prompt":"hello"}` {
		t.Errorf("unexpected row: provider=%s model=%s status=%s data=%s", provider, model, status, data)
	}
}

func TestInsertRequestDetail_Duplicate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS requestDetails (
		id TEXT PRIMARY KEY,
		timestamp TEXT,
		provider TEXT,
		model TEXT,
		connectionId TEXT,
		status TEXT,
		data TEXT
	);`)
	if err != nil {
		t.Fatalf("failed to create requestDetails table: %v", err)
	}

	repo := NewRepo(db)

	err = repo.InsertRequestDetail("req-1", "openai", "gpt-4", "conn-1", "success", `{"a":1}`)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// INSERT OR IGNORE should not error on duplicate
	err = repo.InsertRequestDetail("req-1", "anthropic", "claude-3", "conn-2", "error", `{"a":2}`)
	if err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}

	// Original row should remain
	var provider string
	err = db.QueryRow(`SELECT provider FROM requestDetails WHERE id = ?`, "req-1").Scan(&provider)
	if err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if provider != "openai" {
		t.Errorf("expected original provider 'openai', got '%s'", provider)
	}
}

func TestUpdateConnectionLastUsed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// providerConnections already created in setupTestDB
	repo := NewRepo(db)

	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, name, data, createdAt, updatedAt) VALUES
		('conn-upd-1', 'openai', 'apikey', 'Test Conn', '{"key":"val"}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z');`)
	if err != nil {
		t.Fatalf("failed to seed providerConnection: %v", err)
	}

	err = repo.UpdateConnectionLastUsed("conn-upd-1")
	if err != nil {
		t.Fatalf("UpdateConnectionLastUsed failed: %v", err)
	}

	// Verify lastUsedAt is set and consecutiveUseCount incremented
	var lastUsedAt string
	var consecutiveUseCount int
	err = db.QueryRow(`SELECT lastUsedAt, consecutiveUseCount FROM providerConnections WHERE id = ?`, "conn-upd-1").Scan(&lastUsedAt, &consecutiveUseCount)
	if err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if lastUsedAt == "" {
		t.Error("expected lastUsedAt to be set")
	}
	if consecutiveUseCount != 1 {
		t.Errorf("expected consecutiveUseCount 1, got %d", consecutiveUseCount)
	}
}

func TestUpdateConnectionLastUsed_Increment(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepo(db)

	_, err := db.Exec(`INSERT INTO providerConnections (id, provider, authType, name, data, createdAt, updatedAt) VALUES
		('conn-upd-2', 'anthropic', 'apikey', 'Test Conn 2', '{}', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z');`)
	if err != nil {
		t.Fatalf("failed to seed providerConnection: %v", err)
	}

	repo.UpdateConnectionLastUsed("conn-upd-2")
	repo.UpdateConnectionLastUsed("conn-upd-2")
	repo.UpdateConnectionLastUsed("conn-upd-2")

	var consecutiveUseCount int
	err = db.QueryRow(`SELECT consecutiveUseCount FROM providerConnections WHERE id = ?`, "conn-upd-2").Scan(&consecutiveUseCount)
	if err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if consecutiveUseCount != 3 {
		t.Errorf("expected consecutiveUseCount 3 after 3 calls, got %d", consecutiveUseCount)
	}
}

func TestUpdateConnectionLastUsed_NonExistent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepo(db)

	// Update on non-existent ID should not error (UPDATE with no match is no-op)
	err := repo.UpdateConnectionLastUsed("no-such-connection")
	if err != nil {
		t.Errorf("expected no error for non-existent connection, got %v", err)
	}
}
