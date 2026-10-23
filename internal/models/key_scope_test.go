package models

import "testing"

func TestModelAllowed(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		ref     string
		want    bool
	}{
		{"empty scope allows everything", nil, "anything/at-all", true},
		{"empty slice allows everything", []string{}, "openai/gpt-4", true},
		{"exact provider-qualified match", []string{"antigravity/gemini-x"}, "antigravity/gemini-x", true},
		{"bare call of a qualified entry", []string{"antigravity/gemini-x"}, "gemini-x", true},
		{"different model same provider", []string{"antigravity/gemini-x"}, "antigravity/gemini-y", false},
		{"different provider same model name", []string{"provider-a/gemini-x"}, "provider-b/gemini-x", false},
		{"bare entry matches any provider", []string{"gemini-x"}, "openai/gemini-x", true},
		{"bare entry matches bare call", []string{"gemini-x"}, "gemini-x", true},
		{"case insensitive", []string{"Antigravity/Gemini-X"}, "antigravity/gemini-x", true},
		{"whitespace trimmed", []string{" antigravity/gemini-x "}, "antigravity/gemini-x", true},
		{"one of several entries", []string{"a/one", "a/two", "a/three"}, "a/three", true},
		{"not in several entries", []string{"a/one", "a/two"}, "a/four", false},
		{"empty ref denied when restricted", []string{"a/one"}, "", false},
		{"entry with empty value ignored", []string{"", "a/one"}, "a/two", false},
		{"combo alias allowlisted by name", []string{"my-combo"}, "my-combo", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := &APIKey{AllowedModels: tc.allowed}
			if got := k.ModelAllowed(tc.ref); got != tc.want {
				t.Errorf("ModelAllowed(%q) with %v = %v, want %v", tc.ref, tc.allowed, got, tc.want)
			}
		})
	}
}

func TestModelAllowedNilKey(t *testing.T) {
	var k *APIKey
	if !k.ModelAllowed("anything") {
		t.Error("nil key should be unrestricted")
	}
	if k.Restricted() {
		t.Error("nil key should not report Restricted")
	}
}

func TestDeniedModels(t *testing.T) {
	k := &APIKey{AllowedModels: []string{"a/one", "two"}}
	denied := k.DeniedModels([]string{"a/one", "a/two", "a/three", "two"})
	if len(denied) != 1 || denied[0] != "a/three" {
		t.Errorf("expected only a/three denied, got %v", denied)
	}

	open := &APIKey{}
	if got := open.DeniedModels([]string{"x", "y"}); got != nil {
		t.Errorf("unrestricted key should deny nothing, got %v", got)
	}
}

func TestRestricted(t *testing.T) {
	if (&APIKey{}).Restricted() {
		t.Error("no scope should not be restricted")
	}
	if !(&APIKey{AllowedModels: []string{"a/b"}}).Restricted() {
		t.Error("scope should be restricted")
	}
}

func TestInvalidScopeFailsClosed(t *testing.T) {
	k := &APIKey{ScopeInvalid: true}
	if !k.Restricted() {
		t.Fatal("invalid scope must count as restricted")
	}
	if k.ModelAllowed("anything/at-all") {
		t.Error("invalid scope must deny every model")
	}
	if denied := k.DeniedModels([]string{"a/b", "c/d"}); len(denied) != 2 {
		t.Errorf("expected every ref denied, got %v", denied)
	}
}
