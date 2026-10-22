package promptinject

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func active(prompt string) Settings {
	return Settings{Enabled: true, Mode: "prepend", SystemPrompt: prompt}
}

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode result: %v (%s)", err, body)
	}
	return out
}

func roles(t *testing.T, messages any) []string {
	t.Helper()
	arr, ok := messages.([]any)
	if !ok {
		t.Fatalf("messages is not an array: %#v", messages)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, "?")
			continue
		}
		role, _ := m["role"].(string)
		out = append(out, role)
	}
	return out
}

// ---------------------------------------------------------------- rendering

func TestRenderSubstitutesAllowlistedVariables(t *testing.T) {
	got := Render("You are {{display_name}}. Combo {{combo_name}}. Requested {{requested_model}}.", Vars{
		ComboName:       "support-internal",
		DisplayIdentity: "Support Assistant",
		RequestedModel:  "support",
	})
	want := "You are Support Assistant. Combo support-internal. Requested support."
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestRenderToleratesInnerWhitespaceAndIsCaseSensitive(t *testing.T) {
	got := Render("{{ display_name }}|{{DISPLAY_NAME}}|{{display_name}}", Vars{
		ComboName:       "c",
		DisplayIdentity: "Asa",
	})
	want := "Asa|{{DISPLAY_NAME}}|Asa"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestRenderKeepsUnknownVariablesLiteralAndNeverReExpands(t *testing.T) {
	got := Render("{{provider}} {{api_key}} {{display_name}}", Vars{
		ComboName:       "c",
		DisplayIdentity: "{{api_key}}",
	})
	want := "{{provider}} {{api_key}} {{api_key}}"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestDisplayNameFallsBackToComboNameThenEmpty(t *testing.T) {
	if got := Render("{{display_name}}", Vars{ComboName: "smart"}); got != "smart" {
		t.Fatalf("fallback to combo name = %q", got)
	}
	if got := Render("{{display_name}}", Vars{}); got != "" {
		t.Fatalf("empty fallback = %q", got)
	}
}

// ------------------------------------------------------------- normalization

func TestResolveEnabledLegacyAndExplicit(t *testing.T) {
	zero, one := int64(0), int64(1)
	cases := []struct {
		name   string
		stored *int64
		prompt string
		want   bool
	}{
		{"legacy row with prompt keeps injecting", nil, "hello", true},
		{"legacy row without prompt stays off", nil, "   ", false},
		{"explicit off wins over prompt", &zero, "hello", false},
		{"explicit on without prompt stays off at compose", &one, "", true},
	}
	for _, tc := range cases {
		if got := ResolveEnabled(tc.stored, tc.prompt); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeModeFallsBackToPrepend(t *testing.T) {
	for _, raw := range []string{"", "bogus", "PREPEND", "replace "} {
		if got := NormalizeMode(raw); got != ModePrepend {
			t.Errorf("NormalizeMode(%q) = %q, want prepend", raw, got)
		}
	}
	for _, mode := range Modes {
		if got := NormalizeMode(string(mode)); got != mode {
			t.Errorf("NormalizeMode(%q) = %q", mode, got)
		}
	}
}

// -------------------------------------------------------------- validation

func TestValidateRejectsWrongTypesWithoutCoercion(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr string
	}{
		{"string true is rejected", `{"enabled":"true"}`, "must be a boolean"},
		{"number enabled is rejected", `{"enabled":1}`, "must be a boolean"},
		{"unknown mode is rejected", `{"injectMode":"merge"}`, "Injection strategy must be"},
		{"prompt must be string", `{"systemPrompt":42}`, "systemPrompt must be a string or null"},
		{"identity must be string", `{"displayIdentity":[]}`, "displayIdentity must be a string or null"},
	}
	for _, tc := range cases {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.payload), &payload); err != nil {
			t.Fatalf("%s: bad fixture: %v", tc.name, err)
		}
		_, err := Validate(payload)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want contains %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestValidatePartialUpdateTouchesOnlySuppliedKeys(t *testing.T) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"injectMode":"append"}`), &payload); err != nil {
		t.Fatal(err)
	}
	patch, err := Validate(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.Mode == nil || *patch.Mode != "append" {
		t.Fatalf("mode not captured: %#v", patch.Mode)
	}
	if patch.Enabled != nil || patch.SystemPrompt != nil || patch.DisplayIdentity != nil {
		t.Fatalf("unsupplied fields must stay nil: %#v", patch)
	}
}

func TestValidateAcceptsNullAndEmptyStrings(t *testing.T) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"systemPrompt":null,"displayIdentity":""}`), &payload); err != nil {
		t.Fatal(err)
	}
	patch, err := Validate(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.SystemPrompt == nil || *patch.SystemPrompt != "" {
		t.Fatalf("null prompt should clear to empty: %#v", patch.SystemPrompt)
	}
	if patch.DisplayIdentity == nil || *patch.DisplayIdentity != "" {
		t.Fatalf("empty identity should be accepted: %#v", patch.DisplayIdentity)
	}
}

// ------------------------------------------------------------------- no-ops

func TestApplyReturnsOriginalSliceWhenNothingToDo(t *testing.T) {
	inactive := []Settings{
		{Enabled: false, SystemPrompt: "ignored"},
		{Enabled: true, SystemPrompt: "   \n "},
	}
	bodies := map[string]string{
		"chat":   `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
		"claude": `{"model":"m","system":"sys","messages":[{"role":"user","content":"hi"}]}`,
	}
	for name, raw := range bodies {
		for _, s := range inactive {
			body := []byte(raw)
			out := Apply(body, s, Vars{})
			if &out[0] != &body[0] {
				t.Errorf("%s/%+v: expected untouched original slice", name, s)
			}
		}
	}

	// An active scope with no supported format branch must also stay untouched.
	orphan := []byte(`{"model":"m"}`)
	if out := Apply(orphan, active("hello"), Vars{}); &out[0] != &orphan[0] {
		t.Fatalf("unsupported body shape should be returned unchanged")
	}
}

func TestApplyDoesNotMutateCallerBody(t *testing.T) {
	raw := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	body := []byte(raw)
	out := Apply(body, active("be nice"), Vars{ComboName: "c"})
	if string(body) != raw {
		t.Fatalf("caller body was mutated: %s", body)
	}
	if string(out) == raw {
		t.Fatalf("expected a composed body, got the original")
	}
}

// -------------------------------------------------------------- chat compose

func TestChatComposePlacement(t *testing.T) {
	body := []byte(`{"model":"m","messages":[
		{"role":"system","content":"Client"},
		{"role":"user","content":"Hello"},
		{"role":"assistant","content":"Hi"},
		{"role":"developer","content":"Dev"}
	]}`)

	cases := []struct {
		mode Mode
		want []string
	}{
		{ModePrepend, []string{"system", "system", "user", "assistant", "developer"}},
		{ModeAppend, []string{"system", "user", "assistant", "developer", "system"}},
		{ModeReplace, []string{"system", "user", "assistant"}},
	}
	for _, tc := range cases {
		out := Apply(body, Settings{Enabled: true, Mode: string(tc.mode), SystemPrompt: "Injected"}, Vars{})
		payload := decode(t, out)
		if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: roles = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestChatAppendKeepsInjectionRightAfterLastInstruction(t *testing.T) {
	body := []byte(`{"model":"m","messages":[
		{"role":"system","content":"Client"},
		{"role":"developer","content":"Dev"},
		{"role":"user","content":"Hello"}
	]}`)
	out := Apply(body, Settings{Enabled: true, Mode: "append", SystemPrompt: "Injected"}, Vars{})
	payload := decode(t, out)
	want := []string{"system", "developer", "system", "user"}
	if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
}

func TestChatAppendWithNoInstructionsPrepends(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"Hello"}]}`)
	out := Apply(body, Settings{Enabled: true, Mode: "append", SystemPrompt: "Injected"}, Vars{})
	want := []string{"system", "user"}
	if got := roles(t, decode(t, out)["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
}

func TestChatReplaceKeepsHistoryAndToolResults(t *testing.T) {
	body := []byte(`{"model":"m","messages":[
		{"role":"system","content":"Client"},
		{"role":"user","content":"q"},
		{"role":"assistant","tool_calls":[{"id":"1"}]},
		{"role":"tool","tool_call_id":"1","content":"result"},
		{"role":"developer","content":"Dev"}
	]}`)
	out := Apply(body, Settings{Enabled: true, Mode: "replace", SystemPrompt: "Injected"}, Vars{})
	payload := decode(t, out)
	want := []string{"system", "user", "assistant", "tool"}
	if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	// tool payload must survive untouched
	found := false
	for _, item := range payload["messages"].([]any) {
		if m, ok := item.(map[string]any); ok && m["role"] == "tool" {
			found = m["tool_call_id"] == "1" && m["content"] == "result"
		}
	}
	if !found {
		t.Fatalf("tool message payload was rewritten: %s", out)
	}
}

func TestChatHiddenUserInsertsPairBeforeFirstUser(t *testing.T) {
	body := []byte(`{"model":"m","messages":[
		{"role":"system","content":"Client"},
		{"role":"user","content":"Hello"}
	]}`)
	out := Apply(body, Settings{Enabled: true, Mode: "hidden_user", SystemPrompt: "Be Asa"}, Vars{})
	payload := decode(t, out)
	want := []string{"system", "user", "assistant", "user"}
	if got := roles(t, payload["messages"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	msgs := payload["messages"].([]any)
	assistant, _ := msgs[2].(map[string]any)
	if assistant["content"] != "Understood." {
		t.Fatalf("assistant stub = %v", assistant["content"])
	}
}
