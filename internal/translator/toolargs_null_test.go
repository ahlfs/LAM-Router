package translator

import (
	"strings"
	"testing"
)

// Regresi 400 "tool_use.input: Field required".
//
// Satu tool call tanpa argumen sempat berjalan keluar sebagai arguments:"null"
// (respons) dan "args":null (request ke antigravity). Vertex menerjemahkan
// functionCall menjadi blok tool_use, lalu menolak SELURUH request karena blok
// itu tanpa input — termasuk saat model diganti ke Claude di sesi yang sama,
// karena riwayat percakapan yang memuat tool call itulah yang ditolak. Sesi
// baru tidak memuat riwayat tersebut sehingga tampak "normal".
func TestSanitizeToolArgsNullJadiObjekKosong(t *testing.T) {
	kasus := map[string]string{
		"null":   "{}",
		"":       "{}",
		"   ":    "{}",
		"[1,2]":  "{}",
		"{rusak": "{}",
		"{}":     "{}",
	}
	for masukan, mau := range kasus {
		if got := sanitizeToolArgs("todo_list", masukan); got != mau {
			t.Errorf("sanitizeToolArgs(%q) = %q, mau %q", masukan, got, mau)
		}
	}

	// Argumen yang sah tidak boleh diubah.
	if got := sanitizeToolArgs("todo_list", `{"note":"x"}`); got != `{"note":"x"}` {
		t.Errorf("argumen sah berubah: %q", got)
	}
}

func TestNormalizeToolArgsNullMenjadiMapKosong(t *testing.T) {
	if got := normalizeToolArgs("null"); got == nil || len(got) != 0 {
		t.Errorf(`normalizeToolArgs("null") = %#v, mau map kosong non-nil`, got)
	}
	if got := normalizeToolArgs(""); got == nil || len(got) != 0 {
		t.Errorf(`normalizeToolArgs("") = %#v, mau map kosong non-nil`, got)
	}
	if got := normalizeToolArgs(`{"a":1}`); got["a"] != float64(1) {
		t.Errorf("argumen sah rusak: %#v", got)
	}
}

func TestTranslateOpenAIToGeminiArgsNullJadiObjekKosong(t *testing.T) {
	body := `{
		"model": "claude-sonnet-4-6",
		"messages": [
			{"role": "user", "content": "cek daftar tugas"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "toolu_vrtx_01", "type": "function",
				 "function": {"name": "todo_list", "arguments": "null"}}
			]},
			{"role": "tool", "tool_call_id": "toolu_vrtx_01", "content": "- [ ] a"}
		],
		"tools": [{"type": "function", "function": {
			"name": "todo_list", "description": "lihat daftar tugas",
			"parameters": {"type": "object", "properties": {}}
		}}]
	}`

	out, err := TranslateOpenAIToGemini([]byte(body))
	if err != nil {
		t.Fatalf("TranslateOpenAIToGemini gagal: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `"args":null`) {
		t.Errorf("request masih memuat args null: %s", got)
	}
	if !strings.Contains(got, `"args":{}`) {
		t.Errorf("functionCall kehilangan args objek kosong: %s", got)
	}
}

func TestWrapForAntigravityTakPernahKirimArgsNull(t *testing.T) {
	body := `{"contents":[
		{"role": "model", "parts": [{"functionCall": {"id": "toolu_vrtx_01", "name": "todo_list"}}]},
		{"role": "user", "parts": [{"functionResponse": {"name": "todo_list"}}]}
	]}`

	out, err := WrapForAntigravity([]byte(body), "proyek-uji", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("WrapForAntigravity gagal: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `"args":null`) {
		t.Errorf("body antigravity masih memuat args null: %s", got)
	}
	if !strings.Contains(got, `"args":{}`) {
		t.Errorf("body antigravity tak memuat args objek kosong: %s", got)
	}
	if strings.Contains(got, `"response":null`) {
		t.Errorf("functionResponse masih memuat response null: %s", got)
	}
	// ID sintetis untuk model Claude tetap disisipkan.
	if !strings.Contains(got, `"id":"toolu_`) {
		t.Errorf("ID tool call Claude hilang: %s", got)
	}
}

func TestTranslateGeminiResponseArgsNullJadiObjekKosong(t *testing.T) {
	resp := `{
		"candidates": [{
			"content": {"role": "model", "parts": [{"functionCall": {"name": "todo_list"}}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 7}
	}`

	out, _, err := TranslateGeminiResponseToOpenAI([]byte(resp))
	if err != nil {
		t.Fatalf("TranslateGeminiResponseToOpenAI gagal: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `"arguments":"null"`) {
		t.Errorf("respons masih memuat arguments null: %s", got)
	}
	if !strings.Contains(got, `"arguments":"{}"`) {
		t.Errorf("respons tak memuat arguments objek kosong: %s", got)
	}
}

func TestTranslateGeminiChunkArgsNullJadiObjekKosong(t *testing.T) {
	state := &GeminiStreamState{MessageId: "chatcmpl-uji", Model: "claude-sonnet-4-6"}
	chunk := `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"todo_list"}}]}}]}`

	out, err := TranslateGeminiChunkToOpenAI([]byte(chunk), state)
	if err != nil {
		t.Fatalf("TranslateGeminiChunkToOpenAI gagal: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `arguments\":null`) || strings.Contains(got, `"arguments":"null"`) {
		t.Errorf("chunk masih memuat arguments null: %s", got)
	}
	if !strings.Contains(got, `"arguments":"{}"`) {
		t.Errorf("chunk tak memuat arguments objek kosong: %s", got)
	}
}
