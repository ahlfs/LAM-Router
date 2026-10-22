// Package promptinject implements system-prompt injection for requests entering
// the gateway.
//
// The contract follows the JRouter combo-prompt-injection spec:
//
//   - Settings normalize loosely on the read path (legacy rows, integer flags,
//     unknown modes) and validate strictly on the write path (see Validate).
//   - A template may reference exactly three variables: {{combo_name}},
//     {{display_name}} and {{requested_model}}. Substitution is single-pass and
//     case-sensitive, unknown variables stay literal, and whitespace inside the
//     braces is tolerated.
//   - Composition is a pure function of (body, settings, vars): the caller's
//     bytes are never mutated, and a no-op returns the original slice untouched.
//   - Format branches follow the spec's precedence: Gemini, Claude, Responses,
//     then Chat.
//
// LAM-Router adds one mode the spec does not have: "hidden_user", which pushes
// the instruction as a fake user/assistant exchange because some upstream
// models ignore system messages.
package promptinject

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Mode is an injection placement strategy.
type Mode string

const (
	ModePrepend    Mode = "prepend"
	ModeAppend     Mode = "append"
	ModeReplace    Mode = "replace"
	ModeHiddenUser Mode = "hidden_user"
)

// Modes lists every accepted strategy in a stable order for UI dropdowns and
// error messages.
var Modes = []Mode{ModePrepend, ModeAppend, ModeReplace, ModeHiddenUser}

// ValidMode reports whether raw names a supported strategy.
func ValidMode(raw string) bool {
	switch Mode(raw) {
	case ModePrepend, ModeAppend, ModeReplace, ModeHiddenUser:
		return true
	}
	return false
}

// NormalizeMode maps anything unrecognized (including the empty string) to
// prepend, matching the spec's loose read path.
func NormalizeMode(raw string) Mode {
	if ValidMode(raw) {
		return Mode(raw)
	}
	return ModePrepend
}

// ResolveEnabled derives the effective enabled flag. A nil stored value means
// the row predates the flag, so legacy behaviour applies: a non-blank prompt
// keeps injecting. Any explicit 0/1 wins.
func ResolveEnabled(stored *int64, prompt string) bool {
	if stored != nil {
		return *stored != 0
	}
	return strings.TrimSpace(prompt) != ""
}

// Settings is one scope's injection configuration.
type Settings struct {
	Enabled         bool
	Mode            string
	SystemPrompt    string // raw template, before variable substitution
	DisplayIdentity string // value for {{display_name}}
}

// Vars carries the template values available to a request.
type Vars struct {
	ComboName       string
	DisplayIdentity string
	RequestedModel  string
}

// Format identifies the wire shape of a request body.
type Format string

const (
	FormatUnknown   Format = ""
	FormatChat      Format = "openai-chat"
	FormatClaude    Format = "claude"
	FormatResponses Format = "openai-responses"
	FormatGemini    Format = "gemini"
)

var varPattern = regexp.MustCompile(`\{\{\s*(combo_name|display_name|requested_model)\s*\}\}`)

// Render substitutes the three allowlisted variables in a single pass. Text
// introduced by a variable is never re-expanded, and unknown placeholders are
// left alone.
func Render(template string, vars Vars) string {
	if template == "" {
		return ""
	}
	values := map[string]string{
		"combo_name":      vars.ComboName,
		"display_name":    displayName(vars),
		"requested_model": vars.RequestedModel,
	}
	return varPattern.ReplaceAllStringFunc(template, func(match string) string {
		sub := varPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return values[sub[1]]
	})
}

func displayName(vars Vars) string {
	if dn := strings.TrimSpace(vars.DisplayIdentity); dn != "" {
		return dn
	}
	if name := strings.TrimSpace(vars.ComboName); name != "" {
		return name
	}
	return ""
}

// Validate checks a write payload and reports only the fields actually present,
// so a partial update never clears sibling settings. Values are decoded as raw
// JSON: `"true"` is rejected rather than coerced, and an explicit null clears a
// string field.
func Validate(payload map[string]json.RawMessage) (*Patch, error) {
	if payload == nil {
		return nil, fmt.Errorf("Invalid combo settings")
	}
	patch := &Patch{}

	if raw, ok := payload["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, fmt.Errorf("Prompt injection enabled must be a boolean")
		}
		patch.Enabled = &enabled
	}

	if raw, ok := payload["injectMode"]; ok {
		var mode string
		if err := json.Unmarshal(raw, &mode); err != nil || !ValidMode(mode) {
			return nil, fmt.Errorf("Injection strategy must be prepend, append, replace or hidden_user")
		}
		patch.Mode = &mode
	}

	if raw, ok := payload["systemPrompt"]; ok {
		value, err := nullableString(raw)
		if err != nil {
			return nil, fmt.Errorf("systemPrompt must be a string or null")
		}
		patch.SystemPrompt = &value
	}

	if raw, ok := payload["displayIdentity"]; ok {
		value, err := nullableString(raw)
		if err != nil {
			return nil, fmt.Errorf("displayIdentity must be a string or null")
		}
		patch.DisplayIdentity = &value
	}

	return patch, nil
}

// Patch holds the settings fields a write payload actually named. Nil pointers
// mean "not supplied, keep the stored value".
type Patch struct {
	Enabled         *bool
	Mode            *string
	SystemPrompt    *string
	DisplayIdentity *string
}

// nullableString decodes a JSON string or null. Both map to a Go string; null
// becomes the empty string, which the storage layer writes as an empty value
// (equivalent to the spec's null for these two fields).
func nullableString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("empty value")
	}
	if string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}
