package promptinject

import (
	"encoding/json"
	"strings"
)

// Apply composes the configured prompt into body and returns the new body.
//
// It returns the caller's slice unchanged — same backing array — whenever no
// instruction is dropped: disabled scope, prompt blank after trimming, body that
// is not a JSON object, or no supported format branch. Callers rely on that to
// skip re-hashing and cache invalidation.
//
// hint may be empty. It carries the endpoint's format when the path already
// implies one (e.g. /v1/messages is always Claude); body shape otherwise selects
// the branch, with the spec's precedence: Gemini, Claude, Responses, Chat.
func Apply(body []byte, settings Settings, vars Vars) []byte {
	return ApplyAs(body, settings, vars, FormatUnknown)
}

// ApplyAs is Apply with an explicit endpoint format hint.
func ApplyAs(body []byte, settings Settings, vars Vars, hint Format) []byte {
	settings.Mode = string(NormalizeMode(settings.Mode))
	if !settings.Enabled {
		return body
	}
	if strings.TrimSpace(settings.SystemPrompt) == "" {
		return body
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return body
	}

	// Settings own the identity; a caller-supplied Vars identity is the fallback
	// for scopes that have no stored identity yet.
	if settings.DisplayIdentity != "" {
		vars.DisplayIdentity = settings.DisplayIdentity
	}
	text := Render(settings.SystemPrompt, vars)

	out, ok := compose(payload, text, Mode(settings.Mode), DetectFormat(payload, hint))
	if !ok {
		return body
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return body
	}
	return encoded
}

// DetectFormat picks the composition branch for a body. An endpoint hint for
// Claude or Responses wins; otherwise the payload shape decides, in the spec's
// precedence order.
func DetectFormat(payload map[string]json.RawMessage, hint Format) Format {
	switch hint {
	case FormatClaude, FormatResponses, FormatGemini:
		return hint
	}
	if isJSONArray(payload["contents"]) || truthy(payload["systemInstruction"]) {
		return FormatGemini
	}
	if _, ok := payload["system"]; ok {
		return FormatClaude
	}
	if _, ok := payload["input"]; ok {
		return FormatResponses
	}
	if isJSONArray(payload["messages"]) {
		return FormatChat
	}
	if hint == FormatChat {
		return FormatChat
	}
	return FormatUnknown
}

func compose(payload map[string]json.RawMessage, text string, mode Mode, format Format) (map[string]json.RawMessage, bool) {
	switch format {
	case FormatChat:
		return composeChat(payload, text, mode)
	case FormatClaude:
		return composeClaude(payload, text, mode)
	case FormatResponses:
		return composeResponses(payload, text, mode)
	case FormatGemini:
		return composeGemini(payload, text, mode)
	}
	return nil, false
}

// ---------------------------------------------------------------- OpenAI chat

func composeChat(payload map[string]json.RawMessage, text string, mode Mode) (map[string]json.RawMessage, bool) {
	messages, ok := decodeArray(payload["messages"])
	if !ok {
		return nil, false
	}
	composed, ok := composeMessageList(messages, text, mode)
	if !ok {
		return nil, false
	}
	out := clonePayload(payload)
	encoded, err := json.Marshal(composed)
	if err != nil {
		return nil, false
	}
	out["messages"] = encoded
	return out, true
}

// -------------------------------------------------------------------- Claude

func composeClaude(payload map[string]json.RawMessage, text string, mode Mode) (map[string]json.RawMessage, bool) {
	// hidden_user bypasses the system block entirely: the instruction has to
	// reach the model as conversation, not as a system instruction.
	if mode == ModeHiddenUser {
		messages, ok := decodeArray(payload["messages"])
		if !ok {
			return nil, false
		}
		out := clonePayload(payload)
		encoded, err := json.Marshal(insertHiddenUserPair(messages, text))
		if err != nil {
			return nil, false
		}
		out["messages"] = encoded
		return out, true
	}

	var blocks []json.RawMessage
	switch {
	case isJSONArray(payload["system"]):
		blocks, _ = decodeArray(payload["system"])
	case isJSONString(payload["system"]):
		var existing string
		if err := json.Unmarshal(payload["system"], &existing); err == nil && existing != "" {
			blocks = []json.RawMessage{mustJSON(map[string]any{"type": "text", "text": existing})}
		}
	}

	injection := mustJSON(map[string]any{"type": "text", "text": text})
	switch mode {
	case ModeReplace:
		blocks = []json.RawMessage{injection}
	case ModeAppend:
		blocks = append(blocks, injection)
	default: // prepend
		blocks = append([]json.RawMessage{injection}, blocks...)
	}

	out := clonePayload(payload)
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return nil, false
	}
	out["system"] = encoded

	// replace must also drop client instruction turns so they cannot shadow the
	// injection.
	if mode == ModeReplace {
		if messages, ok := decodeArray(payload["messages"]); ok {
			filtered := make([]json.RawMessage, 0, len(messages))
			for _, m := range messages {
				if !isInstructionMessage(m) {
					filtered = append(filtered, m)
				}
			}
			if encoded, err := json.Marshal(filtered); err == nil {
				out["messages"] = encoded
			}
		}
	}

	return out, true
}

// ----------------------------------------------------------------- Responses

func composeResponses(payload map[string]json.RawMessage, text string, mode Mode) (map[string]json.RawMessage, bool) {
	var items []json.RawMessage
	switch {
	case isJSONArray(payload["input"]):
		items, _ = decodeArray(payload["input"])
	case isJSONString(payload["input"]):
		var s string
		if err := json.Unmarshal(payload["input"], &s); err == nil {
			items = []json.RawMessage{mustJSON(map[string]any{"role": "user", "content": s})}
		}
	}

	out := clonePayload(payload)

	// Fold top-level instructions into the message list, then drop the key so it
	// cannot be forwarded twice.
	if raw, ok := payload["instructions"]; ok {
		var instructions string
		if err := json.Unmarshal(raw, &instructions); err == nil && instructions != "" {
			items = append([]json.RawMessage{mustJSON(map[string]any{"role": "system", "content": instructions})}, items...)
		}
		delete(out, "instructions")
	}

	composed, ok := composeMessageList(items, text, mode)
	if !ok {
		return nil, false
	}
	encoded, err := json.Marshal(composed)
	if err != nil {
		return nil, false
	}
	out["input"] = encoded
	return out, true
}

// -------------------------------------------------------------------- Gemini

func composeGemini(payload map[string]json.RawMessage, text string, mode Mode) (map[string]json.RawMessage, bool) {
	// hidden_user bypasses systemInstruction: the instruction travels as a
	// user/model exchange inside contents.
	if mode == ModeHiddenUser {
		contents, ok := decodeArray(payload["contents"])
		if !ok {
			return nil, false
		}
		pair := []json.RawMessage{
			mustJSON(map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}}),
			mustJSON(map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Understood."}}}),
		}
		out := clonePayload(payload)
		encoded, err := json.Marshal(insertBeforeFirstRole(contents, "user", pair))
		if err != nil {
			return nil, false
		}
		out["contents"] = encoded
		return out, true
	}

	instruction := map[string]json.RawMessage{}
	if raw, ok := payload["systemInstruction"]; ok {
		var existing map[string]json.RawMessage
		if err := json.Unmarshal(raw, &existing); err == nil {
			instruction = existing
		}
	}

	var parts []json.RawMessage
	if raw, ok := instruction["parts"]; ok {
		parts, _ = decodeArray(raw)
	}

	injection := mustJSON(map[string]any{"text": text})
	switch mode {
	case ModeReplace:
		parts = []json.RawMessage{injection}
	case ModeAppend:
		parts = append(parts, injection)
	default: // prepend
		parts = append([]json.RawMessage{injection}, parts...)
	}

	partsJSON, err := json.Marshal(parts)
	if err != nil {
		return nil, false
	}
	instruction["parts"] = partsJSON
	instruction["role"] = mustJSON("user")

	instructionJSON, err := json.Marshal(instruction)
	if err != nil {
		return nil, false
	}

	out := clonePayload(payload)
	out["systemInstruction"] = instructionJSON
	return out, true
}

// --------------------------------------------------------------- msg helpers

// composeMessageList applies a placement mode to a role/content message list.
// Retained messages are passed through as raw JSON, so unaffected turns keep
// every field they arrived with.
func composeMessageList(messages []json.RawMessage, text string, mode Mode) ([]json.RawMessage, bool) {
	switch mode {
	case ModeHiddenUser:
		return insertHiddenUserPair(messages, text), true
	case ModeReplace:
		out := make([]json.RawMessage, 0, len(messages)+1)
		out = append(out, systemMessage(text))
		for _, m := range messages {
			if !isInstructionMessage(m) {
				out = append(out, m)
			}
		}
		return out, true
	case ModeAppend:
		idx := 0
		for i, m := range messages {
			if isInstructionMessage(m) {
				idx = i + 1
			}
		}
		out := make([]json.RawMessage, 0, len(messages)+1)
		out = append(out, messages[:idx]...)
		out = append(out, systemMessage(text))
		out = append(out, messages[idx:]...)
		return out, true
	default: // prepend
		out := make([]json.RawMessage, 0, len(messages)+1)
		out = append(out, systemMessage(text))
		out = append(out, messages...)
		return out, true
	}
}

func insertHiddenUserPair(messages []json.RawMessage, text string) []json.RawMessage {
	pair := []json.RawMessage{
		mustJSON(map[string]any{"role": "user", "content": text}),
		mustJSON(map[string]any{"role": "assistant", "content": "Understood."}),
	}
	return insertBeforeFirstRole(messages, "user", pair)
}

func insertBeforeFirstRole(messages []json.RawMessage, role string, insert []json.RawMessage) []json.RawMessage {
	idx := -1
	for i, m := range messages {
		if messageRole(m) == role {
			idx = i
			break
		}
	}
	if idx < 0 {
		out := make([]json.RawMessage, 0, len(messages)+len(insert))
		out = append(out, messages...)
		return append(out, insert...)
	}
	out := make([]json.RawMessage, 0, len(messages)+len(insert))
	out = append(out, messages[:idx]...)
	out = append(out, insert...)
	return append(out, messages[idx:]...)
}

func systemMessage(text string) json.RawMessage {
	return mustJSON(map[string]any{"role": "system", "content": text})
}

func isInstructionMessage(raw json.RawMessage) bool {
	switch messageRole(raw) {
	case "system", "developer":
		return true
	}
	return false
}

func messageRole(raw json.RawMessage) string {
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Role
}

// ------------------------------------------------------------ json helpers

func decodeArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	return arr, true
}

func isJSONArray(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '['
}

func isJSONString(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '"'
}

func truthy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	switch string(raw) {
	case "null", "false", "0", `""`, "{}", "[]":
		return false
	}
	return true
}

func clonePayload(payload map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
