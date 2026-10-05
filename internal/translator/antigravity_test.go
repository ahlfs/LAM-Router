package translator_test

import (
	"encoding/json"
	"strings"
	"testing"

	"lamrouter/internal/translator"
)

func TestCloakAntigravityRequest_RenamesAndInjectsDecoys(t *testing.T) {
	req := &translator.GeminiRequest{
		Contents: []translator.GeminiContent{
			{
				Role: "model",
				Parts: []translator.GeminiPart{
					{FunctionCall: &translator.GeminiFunctionCall{Name: "execute_code", Args: map[string]any{"code": "ls"}}},
				},
			},
			{
				Role: "user",
				Parts: []translator.GeminiPart{
					{FunctionResponse: &translator.GeminiFunctionResp{Name: "execute_code"}},
				},
			},
		},
		Tools: []translator.GeminiTool{
			{
				FunctionDeclarations: []translator.GeminiFunctionDecl{
					{Name: "execute_code", Description: "Run code"},
					{Name: "run_command", Description: "Native tool"},
				},
			},
		},
	}

	cloaked, toolMap := translator.CloakAntigravityRequest(req, "")
	if cloaked == nil {
		t.Fatal("expected cloaked request, got nil")
	}

	// execute_code should be renamed to execute_code_ide
	if toolMap["execute_code_ide"] != "execute_code" {
		t.Errorf("expected toolMap[execute_code_ide] = execute_code, got %s", toolMap["execute_code_ide"])
	}

	// Check function call in history was renamed
	if cloaked.Contents[0].Parts[0].FunctionCall.Name != "execute_code_ide" {
		t.Errorf("expected contents functionCall renamed to execute_code_ide, got %s", cloaked.Contents[0].Parts[0].FunctionCall.Name)
	}
	if cloaked.Contents[1].Parts[0].FunctionResponse.Name != "execute_code_ide" {
		t.Errorf("expected contents functionResponse renamed to execute_code_ide, got %s", cloaked.Contents[1].Parts[0].FunctionResponse.Name)
	}

	// Check 21 decoy tools injected
	if len(cloaked.Tools) == 0 || len(cloaked.Tools[0].FunctionDeclarations) < 20 {
		t.Errorf("expected >= 20 function declarations including decoys, got %d", len(cloaked.Tools[0].FunctionDeclarations))
	}
}

func TestUncloakToolName(t *testing.T) {
	toolMap := map[string]string{
		"execute_code_ide": "execute_code",
		"custom_tool_ide":  "custom_tool",
	}

	if un := translator.UncloakToolName("execute_code_ide", toolMap); un != "execute_code" {
		t.Errorf("expected execute_code, got %s", un)
	}
	if un := translator.UncloakToolName("run_command", toolMap); un != "run_command" {
		t.Errorf("expected run_command unchanged, got %s", un)
	}
	if un := translator.UncloakToolName("other_ide", nil); un != "other" {
		t.Errorf("expected other (suffix stripped), got %s", un)
	}
}

func TestAntigravityImageModelAndConfig(t *testing.T) {
	if !translator.IsAntigravityImageModel("gemini-3.1-flash-image") {
		t.Error("expected gemini-3.1-flash-image to be image model")
	}
	if !translator.IsAntigravityImageModel("imagen-3.0-generate-002") {
		t.Error("expected imagen-3.0-generate-002 to be image model")
	}
	if translator.IsAntigravityImageModel("gemini-3-flash") {
		t.Error("expected gemini-3-flash NOT to be image model")
	}

	clean, ratio := translator.ParseImageConfig("gemini-3.1-flash-image-16x9")
	if clean != "gemini-3.1-flash-image" || ratio != "16:9" {
		t.Errorf("expected (gemini-3.1-flash-image, 16:9), got (%s, %s)", clean, ratio)
	}

	clean2, ratio2 := translator.ParseImageConfig("gemini-3.1-flash-image-1024x768")
	if clean2 != "gemini-3.1-flash-image" || ratio2 != "4:3" {
		t.Errorf("expected (gemini-3.1-flash-image, 4:3), got (%s, %s)", clean2, ratio2)
	}
}

func TestWrapAntigravityImageRequest(t *testing.T) {
	reqBytes, err := translator.WrapAntigravityImageRequest("A cute cat", "", "proj-123", "gemini-3.1-flash-image", "16:9")
	if err != nil {
		t.Fatalf("WrapAntigravityImageRequest failed: %v", err)
	}
	if len(reqBytes) == 0 {
		t.Fatal("expected non-empty request bytes")
	}

	var req translator.AntigravityRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal wrapper failed: %v", err)
	}
	if req.RequestType != "image_gen" {
		t.Errorf("expected requestType image_gen, got %s", req.RequestType)
	}
	if req.Project != "proj-123" {
		t.Errorf("expected project proj-123, got %s", req.Project)
	}
}

func TestNormalizeAntigravityModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-3-7-sonnet", "claude-sonnet-4-6"},
		{"claude-3.7-sonnet", "claude-sonnet-4-6"},
		{"claude-sonnet-4.6", "claude-sonnet-4-6"},
		{"gemini-3.8-flash", "gemini-3.8-flash-tiered"},
		{"gemini-3.8-flash-high", "gemini-3.8-flash-tiered"},
		{"gemini-3.7-flash", "gemini-3.7-flash-tiered"},
		{"gemini-3.7-flash-high", "gemini-3.7-flash-tiered"},
		{"gemini-3.7-flash-medium", "gemini-3.7-flash-tiered"},
		{"gemini-3.7-flash-low", "gemini-3.7-flash-tiered"},
		{"gemini-3.6-flash-high", "gemini-3.6-flash-tiered"},
		{"gemini-3-flash-agent", "gemini-3-flash-agent"},
		{"ag/gemini-3.8-flash-high", "gemini-3.8-flash-tiered"},
	}

	for _, tt := range tests {
		got := translator.NormalizeAntigravityModel(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeAntigravityModel(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestAntigravityDecoyTools_NonEmptyProperties(t *testing.T) {
	for _, dt := range translator.AntigravityDecoyTools {
		params, ok := dt.Parameters.(map[string]any)
		if !ok {
			t.Fatalf("decoy tool %s has invalid parameters type", dt.Name)
		}
		props, ok := params["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Errorf("decoy tool %s has empty properties; Gemini will reject with 'Invalid tool parameters'", dt.Name)
		}
	}
}

func TestTranslateOpenAIToGemini_ClaudeCodeToolResponseMapping(t *testing.T) {
	body := []byte(`{
		"model": "antigravity/gemini-3.5-flash-high",
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "toolu_01ABC123",
						"type": "function",
						"function": {
							"name": "plugin:claude-mem:mcp-search",
							"arguments": "{\"query\":\"test\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "toolu_01ABC123",
				"content": "{\"results\":[]}"
			}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "plugin:claude-mem:mcp-search",
					"description": "search memory",
					"parameters": {
						"type": "object",
						"properties": {
							"query": { "type": "string" }
						},
						"required": ["query"]
					}
				}
			}
		]
	}`)

	geminiJSON, err := translator.TranslateOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("TranslateOpenAIToGemini failed: %v", err)
	}

	var req translator.GeminiRequest
	if err := json.Unmarshal(geminiJSON, &req); err != nil {
		t.Fatalf("unmarshal gemini request failed: %v", err)
	}

	if len(req.Contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(req.Contents))
	}

	// Tool call part must have ID preserved
	fcPart := req.Contents[0].Parts[0]
	if fcPart.FunctionCall == nil {
		t.Fatal("expected functionCall part")
	}
	if fcPart.FunctionCall.ID != "toolu_01ABC123" {
		t.Errorf("expected functionCall ID 'toolu_01ABC123', got %q", fcPart.FunctionCall.ID)
	}

	// Tool response part must have exact name "plugin:claude-mem:mcp-search"
	respPart := req.Contents[1].Parts[0]
	if respPart.FunctionResponse == nil {
		t.Fatal("expected functionResponse part")
	}
	if respPart.FunctionResponse.Name != "plugin:claude-mem:mcp-search" {
		t.Errorf("expected functionResponse name 'plugin:claude-mem:mcp-search', got %q", respPart.FunctionResponse.Name)
	}
	if respPart.FunctionResponse.ID != "toolu_01ABC123" {
		t.Errorf("expected functionResponse ID 'toolu_01ABC123', got %q", respPart.FunctionResponse.ID)
	}

	// Verify CloakAntigravityRequest also preserves ID
	cloaked, _ := translator.CloakAntigravityRequest(&req, "")
	if cloaked.Contents[0].Parts[0].FunctionCall.ID != "toolu_01ABC123" {
		t.Errorf("expected cloaked functionCall ID 'toolu_01ABC123', got %q", cloaked.Contents[0].Parts[0].FunctionCall.ID)
	}
	if cloaked.Contents[1].Parts[0].FunctionResponse.ID != "toolu_01ABC123" {
		t.Errorf("expected cloaked functionResponse ID 'toolu_01ABC123', got %q", cloaked.Contents[1].Parts[0].FunctionResponse.ID)
	}
}

func TestStripCompetitivePrompts(t *testing.T) {
	req := &translator.GeminiRequest{
		SystemInstruction: &translator.GeminiContent{
			Role: "user",
			Parts: []translator.GeminiPart{
				{Text: "You are a Claude agent, built on Anthropic's Claude Agent SDK. Solve this task."},
			},
		},
		Contents: []translator.GeminiContent{
			{
				Role: "user",
				Parts: []translator.GeminiPart{
					{Text: "You are a Claude agent, built on Anthropic's Claude Agent SDK. Do something."},
				},
			},
		},
	}

	stripped := translator.StripCompetitivePrompts(req)
	if strings.Contains(stripped.SystemInstruction.Parts[0].Text, "Anthropic's Claude Agent SDK") {
		t.Errorf("expected competitive prompt removed from systemInstruction, got %s", stripped.SystemInstruction.Parts[0].Text)
	}
	if strings.Contains(stripped.Contents[0].Parts[0].Text, "Anthropic's Claude Agent SDK") {
		t.Errorf("expected competitive prompt removed from contents, got %s", stripped.Contents[0].Parts[0].Text)
	}
}

func TestNormalizeAntigravityModel_AllSynonymsValid(t *testing.T) {
	validBackendModels := map[string]bool{
		"gemini-3.8-flash-tiered":    true,
		"gemini-3.7-flash-tiered":    true,
		"gemini-3.6-flash-tiered":    true,
		"gemini-3-flash-agent":       true,
		"gemini-pro-agent":           true,
		"gemini-3.1-pro-high":        true,
		"gemini-3.1-pro-low":         true,
		"claude-sonnet-4-6":          true,
		"claude-opus-4-6-thinking":   true,
		"gpt-oss-120b-medium":        true,
		"gemini-3.1-flash-image":     true,
	}

	for alias, targetModel := range translator.AntigravityModelSynonyms {
		if !validBackendModels[targetModel] {
			t.Errorf("Antigravity synonym %q maps to invalid upstream model %q (will cause 404)", alias, targetModel)
		}
	}
}

func TestWrapForAntigravity_ThinkingConfigInjection(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"Hello"}]}]}`)
	
	// Test 3.8 flash high
	wrapped, err := translator.WrapForAntigravity(body, "my-project", "gemini-3.8-flash-high")
	if err != nil {
		t.Fatalf("WrapForAntigravity error: %v", err)
	}
	var env struct {
		Model   string          `json:"model"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(wrapped, &env); err != nil {
		t.Fatalf("Unmarshal envelope error: %v", err)
	}
	if env.Model != "gemini-3.8-flash-tiered" {
		t.Errorf("expected model gemini-3.8-flash-tiered, got %s", env.Model)
	}
	var req struct {
		GenerationConfig struct {
			ThinkingConfig struct {
				ThinkingLevel   string `json:"thinkingLevel"`
				IncludeThoughts bool   `json:"includeThoughts"`
			} `json:"thinkingConfig"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(env.Request, &req); err != nil {
		t.Fatalf("Unmarshal inner request error: %v", err)
	}
	if req.GenerationConfig.ThinkingConfig.ThinkingLevel != "high" {
		t.Errorf("expected thinkingLevel high, got %q", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	}
	if !req.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Errorf("expected includeThoughts true")
	}

	// Test prefix stripping and 3.6 flash
	wrapped36, err := translator.WrapForAntigravity(body, "my-project", "ag/gemini-3.6-flash-low")
	if err != nil {
		t.Fatalf("WrapForAntigravity error: %v", err)
	}
	if err := json.Unmarshal(wrapped36, &env); err != nil {
		t.Fatalf("Unmarshal envelope error: %v", err)
	}
	if env.Model != "gemini-3.6-flash-tiered" {
		t.Errorf("expected model gemini-3.6-flash-tiered, got %s", env.Model)
	}
	if err := json.Unmarshal(env.Request, &req); err != nil {
		t.Fatalf("Unmarshal inner request error: %v", err)
	}
	if req.GenerationConfig.ThinkingConfig.ThinkingLevel != "low" {
		t.Errorf("expected thinkingLevel low, got %q", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	}
}




