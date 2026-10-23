package chat

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"lamrouter/internal/middleware"
	"lamrouter/internal/models"
)

// withKey attaches an authenticated key to a request context, the way the auth
// middleware does for real traffic.
func withKey(k *models.APIKey) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return r.WithContext(context.WithValue(r.Context(), middleware.ApiKeyContextKey, k))
}

func TestModelScopeDenial(t *testing.T) {
	h := &ChatHandler{}

	t.Run("unrestricted key passes", func(t *testing.T) {
		r := withKey(&models.APIKey{})
		info := &ModelInfo{Provider: "antigravity", Model: "gemini-x"}
		if msg := h.ModelScopeDenial(r, "antigravity/gemini-x", info); msg != "" {
			t.Errorf("expected no denial, got %q", msg)
		}
	})

	t.Run("no key in context passes", func(t *testing.T) {
		r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		info := &ModelInfo{Provider: "antigravity", Model: "gemini-x"}
		if msg := h.ModelScopeDenial(r, "antigravity/gemini-x", info); msg != "" {
			t.Errorf("expected no denial without a key, got %q", msg)
		}
	})

	t.Run("allowed model passes", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"antigravity/gemini-x"}})
		info := &ModelInfo{Provider: "antigravity", Model: "gemini-x"}
		if msg := h.ModelScopeDenial(r, "antigravity/gemini-x", info); msg != "" {
			t.Errorf("expected no denial, got %q", msg)
		}
	})

	t.Run("bare call of an allowed qualified model passes", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"antigravity/gemini-x"}})
		info := &ModelInfo{Provider: "antigravity", Model: "gemini-x"}
		if msg := h.ModelScopeDenial(r, "gemini-x", info); msg != "" {
			t.Errorf("expected no denial, got %q", msg)
		}
	})

	t.Run("denied model is refused with the allowed list", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"antigravity/gemini-x"}})
		info := &ModelInfo{Provider: "antigravity", Model: "claude-sonnet"}
		msg := h.ModelScopeDenial(r, "antigravity/claude-sonnet", info)
		if msg == "" {
			t.Fatal("expected a denial")
		}
		if !strings.Contains(msg, "antigravity/claude-sonnet") || !strings.Contains(msg, "antigravity/gemini-x") {
			t.Errorf("denial should name the denied model and the allowed list, got %q", msg)
		}
	})

	t.Run("combo with a denied member is refused", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"antigravity/gemini-x"}})
		info := &ModelInfo{ComboModels: []string{"antigravity/gemini-x", "antigravity/claude-sonnet"}}
		if msg := h.ModelScopeDenial(r, "my-combo", info); msg == "" {
			t.Error("expected a denial for a combo with an out-of-scope member")
		}
	})

	t.Run("combo with all members allowed passes", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"antigravity/gemini-x", "antigravity/claude-sonnet"}})
		info := &ModelInfo{ComboModels: []string{"antigravity/gemini-x", "antigravity/claude-sonnet"}}
		if msg := h.ModelScopeDenial(r, "my-combo", info); msg != "" {
			t.Errorf("expected no denial, got %q", msg)
		}
	})

	t.Run("combo allowlisted by name passes regardless of members", func(t *testing.T) {
		r := withKey(&models.APIKey{AllowedModels: []string{"my-combo"}})
		info := &ModelInfo{ComboModels: []string{"a/one", "b/two"}}
		if msg := h.ModelScopeDenial(r, "my-combo", info); msg != "" {
			t.Errorf("expected no denial, got %q", msg)
		}
	})
}
