package chat

import (
	"fmt"
	"net/http"
	"strings"

	"lamrouter/internal/middleware"
)

// ModelScopeDenial reports why a request may not use the resolved model, or ""
// when the key's scope allows it.
//
// A single-model request passes when the model it will actually be forwarded as
// is allowlisted (the requested spelling is accepted too, so aliases and
// provider-qualified names both work). A combo request passes when the combo
// name itself is allowlisted, or when every member model is inside the scope —
// otherwise the cascade could reach a model the operator never granted.
func (h *ChatHandler) ModelScopeDenial(r *http.Request, requested string, info *ModelInfo) string {
	if info == nil {
		return ""
	}
	key := middleware.GetAuthenticatedApiKey(r)
	if !key.Restricted() {
		return ""
	}
	if key.ModelAllowed(requested) {
		return ""
	}

	var refs []string
	if len(info.ComboModels) > 0 {
		refs = append(refs, info.ComboModels...)
	} else if info.Provider != "" && info.Model != "" {
		refs = append(refs, info.Provider+"/"+info.Model)
	} else if info.Model != "" {
		refs = append(refs, info.Model)
	} else {
		refs = append(refs, requested)
	}

	denied := key.DeniedModels(refs)
	if len(denied) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Model %q is outside this API key's allowed models. Allowed: %s.",
		strings.Join(denied, ", "), strings.Join(key.AllowedModels, ", "),
	)
}
