package models

import "strings"

// Model scope matching for API keys ("Restricted" scope in the dashboard).
//
// The stored entries come from the model picker, which emits provider-qualified
// ids ("antigravity/gemini-3.8-flash-low"), but clients often call the bare
// name. Matching therefore compares the provider and the model separately:
//
//	allowed "antigravity/gemini-x"  matches "antigravity/gemini-x" and "gemini-x"
//	allowed "gemini-x"              matches "gemini-x" and any provider's "…/gemini-x"
//	allowed "provider-a/gemini-x"   never matches "provider-b/gemini-x"
//
// Comparison is case-insensitive and whitespace-trimmed.

// NormalizeModelRef lowercases and trims a model reference.
func NormalizeModelRef(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// splitModelRef splits "provider/model" at the last slash. A ref without a
// slash yields an empty provider.
func splitModelRef(s string) (provider, model string) {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// ModelAllowed reports whether ref is inside this key's scope. A key with no
// scope is unrestricted. A nil key is treated as unrestricted so call sites do
// not need a nil check before calling.
func (k *APIKey) ModelAllowed(ref string) bool {
	if k == nil {
		return true
	}
	if k.ScopeInvalid {
		return false
	}
	if len(k.AllowedModels) == 0 {
		return true
	}
	r := NormalizeModelRef(ref)
	if r == "" {
		return false
	}
	rProv, rModel := splitModelRef(r)
	if rModel == "" {
		return false
	}
	for _, allowed := range k.AllowedModels {
		a := NormalizeModelRef(allowed)
		if a == "" {
			continue
		}
		if a == r {
			return true
		}
		aProv, aModel := splitModelRef(a)
		if aModel != rModel {
			continue
		}
		// A bare allowlist entry matches the model regardless of provider; a
		// provider-qualified entry only matches that provider (or a caller that
		// omitted the provider prefix).
		if aProv == "" || rProv == "" || aProv == rProv {
			return true
		}
	}
	return false
}

// Restricted reports whether this key has a model scope at all.
func (k *APIKey) Restricted() bool {
	return k != nil && (len(k.AllowedModels) > 0 || k.ScopeInvalid)
}

// DeniedModels returns the refs that fall outside the key's scope, preserving
// input order. An unrestricted key returns nil.
func (k *APIKey) DeniedModels(refs []string) []string {
	if k == nil || (!k.ScopeInvalid && len(k.AllowedModels) == 0) {
		return nil
	}
	var denied []string
	for _, ref := range refs {
		if !k.ModelAllowed(ref) {
			denied = append(denied, ref)
		}
	}
	return denied
}
