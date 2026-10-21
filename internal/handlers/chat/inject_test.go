package chat

import (
	"encoding/json"
	"testing"
)

func messages(t *testing.T, pairs ...string) []byte {
	t.Helper()
	var msgs []map[string]string
	for i := 0; i+1 < len(pairs); i += 2 {
		msgs = append(msgs, map[string]string{"role": pairs[i], "content": pairs[i+1]})
	}
	b, _ := json.Marshal(map[string]any{"model": "test/m", "messages": msgs})
	return b
}

func getMessages(t *testing.T, body []byte) []map[string]string {
	t.Helper()
	var p map[string]json.RawMessage
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var msgs []map[string]string
	if err := json.Unmarshal(p["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	return msgs
}

func TestInjectSystemPrompt_EmptyPrompt(t *testing.T) {
	body := messages(t, "user", "hello")
	out := InjectSystemPrompt(body, "", "prepend")
	if string(out) != string(body) {
		t.Error("empty prompt must be a no-op")
	}
}

func TestInjectSystemPrompt_PrependNoExisting(t *testing.T) {
	body := messages(t, "user", "hello")
	out := InjectSystemPrompt(body, "Be helpful.", "prepend")
	msgs := getMessages(t, out)
	if msgs[0]["role"] != "system" {
		t.Fatalf("expected system first, got %q", msgs[0]["role"])
	}
	if msgs[0]["content"] != "Be helpful." {
		t.Errorf("unexpected system content: %q", msgs[0]["content"])
	}
	if msgs[1]["role"] != "user" {
		t.Errorf("expected user second, got %q", msgs[1]["role"])
	}
}

func TestInjectSystemPrompt_PrependWithExisting(t *testing.T) {
	body := messages(t, "system", "You are an assistant.", "user", "hi")
	out := InjectSystemPrompt(body, "Always be concise.", "prepend")
	msgs := getMessages(t, out)
	want := "Always be concise.\n\nYou are an assistant."
	if msgs[0]["content"] != want {
		t.Errorf("prepend got %q want %q", msgs[0]["content"], want)
	}
}

func TestInjectSystemPrompt_Append(t *testing.T) {
	body := messages(t, "system", "You are an assistant.", "user", "hi")
	out := InjectSystemPrompt(body, "Speak in Indonesian.", "append")
	msgs := getMessages(t, out)
	want := "You are an assistant.\n\nSpeak in Indonesian."
	if msgs[0]["content"] != want {
		t.Errorf("append got %q want %q", msgs[0]["content"], want)
	}
}

func TestInjectSystemPrompt_Replace(t *testing.T) {
	body := messages(t, "system", "old prompt", "user", "hi")
	out := InjectSystemPrompt(body, "new prompt", "replace")
	msgs := getMessages(t, out)
	if msgs[0]["content"] != "new prompt" {
		t.Errorf("replace got %q", msgs[0]["content"])
	}
}

func TestInjectSystemPrompt_HiddenUser_NoUserMsg(t *testing.T) {
	body := messages(t, "system", "sys")
	out := InjectSystemPrompt(body, "secret instruction", "hidden_user")
	msgs := getMessages(t, out)
	// pair appended after system
	last2 := msgs[len(msgs)-2:]
	if last2[0]["role"] != "user" || last2[0]["content"] != "secret instruction" {
		t.Errorf("expected injected user, got %+v", last2[0])
	}
	if last2[1]["role"] != "assistant" || last2[1]["content"] != "Understood." {
		t.Errorf("expected assistant ack, got %+v", last2[1])
	}
}

func TestInjectSystemPrompt_HiddenUser_BeforeFirstUser(t *testing.T) {
	body := messages(t, "system", "sys", "user", "real question")
	out := InjectSystemPrompt(body, "secret", "hidden_user")
	msgs := getMessages(t, out)
	// should be: system, injected-user, assistant-ack, real-user
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "secret" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	if msgs[2]["role"] != "assistant" {
		t.Errorf("msgs[2] = %+v", msgs[2])
	}
	if msgs[3]["role"] != "user" || msgs[3]["content"] != "real question" {
		t.Errorf("msgs[3] = %+v", msgs[3])
	}
}

func TestInjectSystemPrompt_DefaultMode(t *testing.T) {
	// mode kosong → prepend
	body := messages(t, "system", "base", "user", "hi")
	out := InjectSystemPrompt(body, "inject", "")
	msgs := getMessages(t, out)
	if msgs[0]["content"] != "inject\n\nbase" {
		t.Errorf("default mode: %q", msgs[0]["content"])
	}
}

func TestInjectSystemPrompt_InvalidBody(t *testing.T) {
	body := []byte("not json")
	out := InjectSystemPrompt(body, "inject", "prepend")
	if string(out) != "not json" {
		t.Error("invalid body must be returned unchanged")
	}
}
