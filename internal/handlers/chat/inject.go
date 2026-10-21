package chat

import (
	"encoding/json"
)

// InjectSystemPrompt memodifikasi payload JSON chat completions dengan
// menambahkan systemPrompt ke dalam messages sesuai mode:
//
//   - "prepend"     – gabungkan inject + "\n\n" + system lama (default)
//   - "append"      – gabungkan system lama + "\n\n" + inject
//   - "replace"     – ganti system lama sepenuhnya dengan inject
//   - "hidden_user" – sisipkan {"role":"user","content":inject} +
//     {"role":"assistant","content":"Understood."} sebelum
//     user turn pertama (berguna untuk model yang mengabaikan system prompt)
//
// Jika systemPrompt kosong atau body bukan JSON object yang valid,
// body dikembalikan tidak berubah.
// Fungsi ini bersifat pure: body asli tidak dimodifikasi.
func InjectSystemPrompt(body []byte, systemPrompt string, mode string) []byte {
	if len(systemPrompt) == 0 {
		return body
	}
	if mode == "" {
		mode = "prepend"
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	rawMsgs, ok := payload["messages"]
	if !ok {
		return body
	}

	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(rawMsgs, &messages); err != nil {
		return body
	}

	switch mode {
	case "prepend", "append", "replace":
		messages = injectIntoSystemMsg(messages, systemPrompt, mode)
	case "hidden_user":
		messages = injectHiddenUser(messages, systemPrompt)
	default:
		messages = injectIntoSystemMsg(messages, systemPrompt, "prepend")
	}

	newMsgs, err := json.Marshal(messages)
	if err != nil {
		return body
	}
	payload["messages"] = json.RawMessage(newMsgs)

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// injectIntoSystemMsg menangani mode prepend / append / replace.
// Jika tidak ada pesan role:system, pesan baru disisipkan di awal.
func injectIntoSystemMsg(messages []map[string]json.RawMessage, inject string, mode string) []map[string]json.RawMessage {
	// cari pesan system pertama
	sysIdx := -1
	for i, m := range messages {
		var role string
		if r, ok := m["role"]; ok {
			_ = json.Unmarshal(r, &role)
		}
		if role == "system" {
			sysIdx = i
			break
		}
	}

	if sysIdx == -1 {
		// tidak ada system message — sisipkan baru di depan
		sysMsg := map[string]json.RawMessage{
			"role":    mustMarshal("system"),
			"content": mustMarshal(inject),
		}
		return append([]map[string]json.RawMessage{sysMsg}, messages...)
	}

	// baca konten existing
	var existingContent string
	if c, ok := messages[sysIdx]["content"]; ok {
		_ = json.Unmarshal(c, &existingContent)
	}

	var final string
	switch mode {
	case "replace":
		final = inject
	case "append":
		if existingContent == "" {
			final = inject
		} else {
			final = existingContent + "\n\n" + inject
		}
	default: // prepend
		if existingContent == "" {
			final = inject
		} else {
			final = inject + "\n\n" + existingContent
		}
	}

	messages[sysIdx]["content"] = mustMarshal(final)
	return messages
}

// injectHiddenUser menyisipkan pasangan user/assistant sebelum turn pertama
// role:user. Berguna untuk model yang mengabaikan system prompt.
func injectHiddenUser(messages []map[string]json.RawMessage, inject string) []map[string]json.RawMessage {
	firstUser := -1
	for i, m := range messages {
		var role string
		if r, ok := m["role"]; ok {
			_ = json.Unmarshal(r, &role)
		}
		if role == "user" {
			firstUser = i
			break
		}
	}

	pair := []map[string]json.RawMessage{
		{"role": mustMarshal("user"), "content": mustMarshal(inject)},
		{"role": mustMarshal("assistant"), "content": mustMarshal("Understood.")},
	}

	if firstUser == -1 {
		// tidak ada user message, tempel di akhir
		return append(messages, pair...)
	}

	// sisipkan sebelum user pertama
	out := make([]map[string]json.RawMessage, 0, len(messages)+2)
	out = append(out, messages[:firstUser]...)
	out = append(out, pair...)
	out = append(out, messages[firstUser:]...)
	return out
}

func mustMarshal(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
