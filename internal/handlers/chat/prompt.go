package chat

import "lamrouter/internal/promptinject"

// applyPromptInjection composes the prompt scopes that apply to one request.
//
// Order is global first, then the combo scope, so a combo instruction stacks on
// top of the gateway-wide one (each prepend puts its text in front of what is
// already there). Both are applied exactly once per request, before any retry or
// fallback dispatch, so member attempts never accumulate copies.
//
// A scope that is disabled, has a blank prompt, or has nothing to compose into
// returns the body untouched.
func (h *ChatHandler) applyPromptInjection(body []byte, info *ModelInfo, requestedModel string, hint promptinject.Format) []byte {
	if info == nil {
		return body
	}
	vars := promptinject.Vars{
		ComboName:       info.ComboName,
		DisplayIdentity: info.DisplayIdentity,
		RequestedModel:  requestedModel,
	}

	if rawDB := h.Repo.RawDB(); rawDB != nil {
		var globalPrompt, globalMode string
		_ = rawDB.QueryRow("SELECT value FROM system_settings WHERE key = 'global_system_prompt'").Scan(&globalPrompt)
		_ = rawDB.QueryRow("SELECT value FROM system_settings WHERE key = 'global_inject_mode'").Scan(&globalMode)
		global := promptinject.Settings{
			Enabled:      promptinject.ResolveEnabled(nil, globalPrompt),
			Mode:         globalMode,
			SystemPrompt: globalPrompt,
		}
		body = promptinject.ApplyAs(body, global, vars, hint)
	}

	if info.InjectEnabled || info.SystemPrompt != "" {
		scope := promptinject.Settings{
			Enabled:         info.InjectEnabled,
			Mode:            info.InjectMode,
			SystemPrompt:    info.SystemPrompt,
			DisplayIdentity: info.DisplayIdentity,
		}
		body = promptinject.ApplyAs(body, scope, vars, hint)
	}

	return body
}
