package promptinject

import (
	"encoding/json"
	"reflect"
	"testing"
)

func applyMode(t *testing.T, raw, mode, prompt string, vars Vars) map[string]any {
	t.Helper()
	out := Apply([]byte(raw), Settings{Enabled: true, Mode: mode, SystemPrompt: prompt}, vars)
	return decode(t, out)
}

// ------------------------------------------------------------------- Claude

func TestClaudeKeepsSystemBlocksAndCacheControl(t *testing.T) {
	raw := `{"model":"m","system":[
		{"type":"text","text":"Client","cache_control":{"type":"ephemeral"}}
	],"messages":[{"role":"user","content":"hi"}]}`

	for _, tc := range []struct {
		mode      string
		wantTexts []string
	}{
		{"prepend", []string{"Injected", "Client"}},
		{"append", []string{"Client", "Injected"}},
	} {
		payload := applyMode(t, raw, tc.mode, "Injected", Vars{})
		blocks, ok := payload["system"].([]any)
		if !ok {
			t.Fatalf("%s: system is not a block array: %#v", tc.mode, payload["system"])
		}
		var texts []string
		for _, b := range blocks {
			if m, ok := b.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					texts = append(texts, s)
				}
			}
		}
		if !reflect.DeepEqual(texts, tc.wantTexts) {
			t.Errorf("%s: system texts = %v, want %v", tc.mode, texts, tc.wantTexts)
		}
		if tc.mode == "append" {
			first, _ := blocks[0].(map[string]any)
			if _, ok := first["cache_control"]; !ok {
				t.Errorf("cache_control was dropped from the preserved block: %#v", first)
			}
		}
	}
}

func TestClaudeStringSystemBecomesBlock(t *testing.T) {
	payload := applyMode(t, `{"model":"m","system":"Client","messages":[{"role":"user","content":"hi"}]}`, "append", "Injected", Vars{})
	blocks, ok := payload["system"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("system = %#v, want two blocks", payload["system"])
	}
	first, _ := blocks[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "Client" {
		t.Fatalf("string system not converted to a text block: %#v", first)
	}
}

func TestClaudeReplaceDropsSystemAndDeveloperTurns(t *testing.T) {
	raw := `{"model":"m","system":"Client","messages":[
		{"role":"system","content":"inline"},
		{"role":"user","content":"hi"},
		{"role":"developer","content":"dev"},
		{"role":"assistant","content":"ok"}
	]}`
	payload := applyMode(t, raw, "replace", "Injected", Vars{})
	blocks, _ := payload["system"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("system blocks = %d, want only the injection", len(blocks))
	}
	want := []string{"user", "assistant"}
	if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
}

func TestClaudeEndpointHintWorksWithoutSystemKey(t *testing.T) {
	raw := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	out := ApplyAs([]byte(raw), active("Injected"), Vars{}, FormatClaude)
	payload := decode(t, out)
	if _, ok := payload["system"]; !ok {
		t.Fatalf("claude hint should create a system block, got %s", out)
	}
}

func TestClaudeHiddenUserUsesConversationPair(t *testing.T) {
	raw := `{"model":"m","system":"Client","messages":[{"role":"user","content":"hi"}]}`
	payload := applyMode(t, raw, "hidden_user", "Be Asa", Vars{})
	want := []string{"user", "assistant", "user"}
	if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	if payload["system"] != "Client" {
		t.Fatalf("system must stay untouched in hidden_user mode: %#v", payload["system"])
	}
}

// ---------------------------------------------------------------- Responses

func TestResponsesFoldsInstructionsAndDropsKey(t *testing.T) {
	raw := `{"model":"m","instructions":"Client rules","input":[
		{"role":"user","content":"hi"},
		{"type":"function_call_output","call_id":"1","output":"ok"}
	]}`
	payload := applyMode(t, raw, "replace", "Injected", Vars{})
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("instructions key should be removed")
	}
	items, ok := payload["input"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("input = %#v, want injection + user + function item", payload["input"])
	}
	first, _ := items[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "Injected" {
		t.Fatalf("first item = %#v, want the injection", first)
	}
	second, _ := items[1].(map[string]any)
	if second["content"] != "hi" {
		t.Fatalf("user item missing: %#v", second)
	}
}

func TestResponsesStringInputBecomesUserMessage(t *testing.T) {
	payload := applyMode(t, `{"model":"m","input":"hello"}`, "prepend", "Injected", Vars{})
	items, _ := payload["input"].([]any)
	if len(items) != 2 {
		t.Fatalf("input = %#v", payload["input"])
	}
	user, _ := items[1].(map[string]any)
	if user["role"] != "user" || user["content"] != "hello" {
		t.Fatalf("string input not converted: %#v", user)
	}
}

// ------------------------------------------------------------------- Gemini

func TestGeminiComposesSystemPartsAndKeepsContents(t *testing.T) {
	raw := `{"model":"m","systemInstruction":{"parts":[{"text":"Client"}]},"contents":[
		{"role":"user","parts":[{"text":"describe"},{"inlineData":{"mimeType":"image/png","data":"AAA"}}]}
	]}`
	payload := applyMode(t, raw, "append", "Injected", Vars{})

	instruction, ok := payload["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction = %#v", payload["systemInstruction"])
	}
	if instruction["role"] != "user" {
		t.Fatalf("role = %v, want user", instruction["role"])
	}
	parts, _ := instruction["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	last, _ := parts[1].(map[string]any)
	if last["text"] != "Injected" {
		t.Fatalf("injection missing from parts: %#v", last)
	}

	contents, _ := payload["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents must be untouched, got %#v", contents)
	}
	entry, _ := contents[0].(map[string]any)
	parts2, _ := entry["parts"].([]any)
	if len(parts2) != 2 {
		t.Fatalf("multimedia contents were rewritten: %#v", entry)
	}
}

func TestGeminiHiddenUserInsertsUserModelPair(t *testing.T) {
	payload := applyMode(t, `{"model":"m","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, "hidden_user", "Be Asa", Vars{})
	contents, _ := payload["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %#v, want pair + original", contents)
	}
	second, _ := contents[1].(map[string]any)
	if second["role"] != "model" {
		t.Fatalf("second entry role = %v, want model", second["role"])
	}
}

// ------------------------------------------------------------------ routing

func TestApplyOnlyRewritesTemplateFieldsNotRoutingIdentity(t *testing.T) {
	raw := `{"model":"support","temperature":0.2,"messages":[{"role":"user","content":"hi"}]}`
	payload := applyMode(t, raw, "prepend", "Combo {{combo_name}} as {{display_name}}", Vars{
		ComboName:       "support-internal",
		DisplayIdentity: "Support Assistant",
		RequestedModel:  "support",
	})

	if payload["model"] != "support" {
		t.Fatalf("model was rewritten: %v", payload["model"])
	}
	if payload["temperature"] != 0.2 {
		t.Fatalf("unrelated field was rewritten: %v", payload["temperature"])
	}
	msgs := payload["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if first["content"] != "Combo support-internal as Support Assistant" {
		t.Fatalf("template not rendered: %#v", first["content"])
	}
}

func TestApplyIsNotIdempotentByDesign(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	once := Apply(body, active("Injected"), Vars{})
	twice := Apply(once, active("Injected"), Vars{})
	if got := roles(t, decode(t, twice)["messages"]); len(got) != 3 {
		t.Fatalf("second application should add a second injection, roles = %v", got)
	}
}

func TestComposedBodyStaysValidJSON(t *testing.T) {
	cases := []map[string]json.RawMessage{}
	_ = cases
	raws := []string{
		`{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","system":[{"type":"text","text":"x"}],"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","instructions":"i","input":"hello"}`,
		`{"model":"m","systemInstruction":{"parts":[{"text":"x"}]},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	}
	for _, raw := range raws {
		for _, mode := range Modes {
			out := Apply([]byte(raw), Settings{Enabled: true, Mode: string(mode), SystemPrompt: "Injected"}, Vars{})
			var probe any
			if err := json.Unmarshal(out, &probe); err != nil {
				t.Errorf("%s/%s produced invalid JSON: %v (%s)", raw, mode, err, out)
			}
		}
	}
}
