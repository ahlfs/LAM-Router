// Package gatewayprefs stores the operator preferences that actually change
// gateway behaviour. Values live in system_settings as one JSON blob so a partial
// write can never wipe a sibling field, and every reader goes through Load so the
// defaults are identical everywhere.
package gatewayprefs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SettingsKey is the system_settings row holding the JSON blob.
const SettingsKey = "gateway_preferences"

// LoggingLevelFull keeps request/response payloads in the audit detail table.
// LoggingLevelMetadata keeps only metadata (ids, tokens, latency, status).
const (
	LoggingLevelFull     = "full"
	LoggingLevelMetadata = "metadata"
	LoggingLevelDisabled = "disabled"
)

// Preferences is the server-side subset of the Settings page: audit retention
// and logging detail, token accounting, and the Playground defaults the
// dashboard and the Playground page both read.
type Preferences struct {
	LogRetentionDays int    `json:"logRetentionDays"`
	LoggingLevel     string `json:"loggingLevel"`
	RecordTokenUsage bool   `json:"recordTokenUsage"`

	DefaultTemperature  float64 `json:"defaultTemperature"`
	DefaultTopP         float64 `json:"defaultTopP"`
	DefaultMaxTokens    int     `json:"defaultMaxTokens"`
	DefaultSystemPrompt string  `json:"defaultSystemPrompt"`
	StreamResponse      bool    `json:"streamResponse"`
}

// Defaults returns the values a fresh install uses.
func Defaults() Preferences {
	return Preferences{
		LogRetentionDays:    30,
		LoggingLevel:        LoggingLevelFull,
		RecordTokenUsage:    true,
		DefaultTemperature:  0.7,
		DefaultTopP:         0.95,
		DefaultMaxTokens:    4096,
		DefaultSystemPrompt: "",
		StreamResponse:      true,
	}
}

// ValidLoggingLevel reports whether the dashboard may store this level.
func ValidLoggingLevel(level string) bool {
	switch level {
	case LoggingLevelFull, LoggingLevelMetadata, LoggingLevelDisabled:
		return true
	}
	return false
}

// Normalize applies defaults for unset values and rejects out-of-range input.
func Normalize(p Preferences) (Preferences, error) {
	if p.LogRetentionDays == 0 {
		p.LogRetentionDays = Defaults().LogRetentionDays
	}
	if p.LogRetentionDays < 1 || p.LogRetentionDays > 3650 {
		return Preferences{}, fmt.Errorf("logRetentionDays must be between 1 and 3650")
	}
	if p.LoggingLevel == "" {
		p.LoggingLevel = Defaults().LoggingLevel
	}
	p.LoggingLevel = strings.ToLower(strings.TrimSpace(p.LoggingLevel))
	if !ValidLoggingLevel(p.LoggingLevel) {
		return Preferences{}, fmt.Errorf("loggingLevel must be %q, %q or %q", LoggingLevelFull, LoggingLevelMetadata, LoggingLevelDisabled)
	}
	if p.DefaultTemperature < 0 || p.DefaultTemperature > 2 {
		return Preferences{}, fmt.Errorf("defaultTemperature must be between 0 and 2")
	}
	if p.DefaultTopP < 0 || p.DefaultTopP > 1 {
		return Preferences{}, fmt.Errorf("defaultTopP must be between 0 and 1")
	}
	if p.DefaultMaxTokens == 0 {
		p.DefaultMaxTokens = Defaults().DefaultMaxTokens
	}
	if p.DefaultMaxTokens < 1 || p.DefaultMaxTokens > 200000 {
		return Preferences{}, fmt.Errorf("defaultMaxTokens must be between 1 and 200000")
	}
	return p, nil
}

// Load reads the stored preferences, falling back to defaults for a missing or
// unreadable row. A corrupt blob must never take the gateway down.
func Load(db *sql.DB) Preferences {
	if db == nil {
		return Defaults()
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM system_settings WHERE key = ?", SettingsKey).Scan(&raw); err != nil {
		return Defaults()
	}
	stored := Defaults()
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return Defaults()
	}
	normalized, err := Normalize(stored)
	if err != nil {
		return Defaults()
	}
	return normalized
}

// Save validates and persists the full preference set. The boolean is taken
// literally: only Apply (patch over Load) should be used by write endpoints, so
// an omitted flag can never silently disable telemetry.
func Save(db *sql.DB, p Preferences) (Preferences, error) {
	normalized, err := Normalize(p)
	if err != nil {
		return Preferences{}, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return Preferences{}, err
	}
	if _, err := db.Exec("INSERT OR REPLACE INTO system_settings (key, value) VALUES (?, ?)", SettingsKey, string(encoded)); err != nil {
		return Preferences{}, err
	}
	return normalized, nil
}

// Patch holds the fields a partial update actually supplied.
type Patch struct {
	LogRetentionDays *int    `json:"logRetentionDays"`
	LoggingLevel     *string `json:"loggingLevel"`
	RecordTokenUsage *bool   `json:"recordTokenUsage"`

	DefaultTemperature  *float64 `json:"defaultTemperature"`
	DefaultTopP         *float64 `json:"defaultTopP"`
	DefaultMaxTokens    *int     `json:"defaultMaxTokens"`
	DefaultSystemPrompt *string  `json:"defaultSystemPrompt"`
	StreamResponse      *bool    `json:"streamResponse"`
}

// Apply merges a patch over the stored values. Supplied values are validated
// before merging, so an explicit 0 window is rejected instead of being read as
// "unset" the way a full-struct Normalize would.
func Apply(db *sql.DB, patch Patch) (Preferences, error) {
	if patch.LogRetentionDays != nil && (*patch.LogRetentionDays < 1 || *patch.LogRetentionDays > 3650) {
		return Preferences{}, fmt.Errorf("logRetentionDays must be between 1 and 3650")
	}
	if patch.LoggingLevel != nil {
		level := strings.ToLower(strings.TrimSpace(*patch.LoggingLevel))
		if !ValidLoggingLevel(level) {
			return Preferences{}, fmt.Errorf("loggingLevel must be %q, %q or %q", LoggingLevelFull, LoggingLevelMetadata, LoggingLevelDisabled)
		}
		patch.LoggingLevel = &level
	}

	current := Load(db)
	if patch.LogRetentionDays != nil {
		current.LogRetentionDays = *patch.LogRetentionDays
	}
	if patch.LoggingLevel != nil {
		current.LoggingLevel = *patch.LoggingLevel
	}
	if patch.RecordTokenUsage != nil {
		current.RecordTokenUsage = *patch.RecordTokenUsage
	}
	if patch.DefaultTemperature != nil {
		current.DefaultTemperature = *patch.DefaultTemperature
	}
	if patch.DefaultTopP != nil {
		current.DefaultTopP = *patch.DefaultTopP
	}
	if patch.DefaultMaxTokens != nil {
		current.DefaultMaxTokens = *patch.DefaultMaxTokens
	}
	if patch.DefaultSystemPrompt != nil {
		current.DefaultSystemPrompt = *patch.DefaultSystemPrompt
	}
	if patch.StreamResponse != nil {
		current.StreamResponse = *patch.StreamResponse
	}
	return Save(db, current)
}
