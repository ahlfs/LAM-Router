package models

// Meta represents a key-value meta record.
type Meta struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Setting represents a single settings record.
type Setting struct {
	ID   int    `json:"id"`
	Data string `json:"data"` // JSON string representation of the settings data
}

// ProviderConnection represents an upstream provider connection.
type ProviderConnection struct {
	ID        string  `json:"id"`
	Provider  string  `json:"provider"`
	AuthType  string  `json:"authType"`
	Name      *string `json:"name,omitempty"`
	Email     *string `json:"email,omitempty"`
	Priority  *int    `json:"priority,omitempty"`
	IsActive  int     `json:"isActive"` // 0 or 1
	Data      string  `json:"data"`     // JSON string representing additional provider config
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// ProviderNode represents a deployment node / executor config.
type ProviderNode struct {
	ID        string  `json:"id"`
	Type      *string `json:"type,omitempty"`
	Name      *string `json:"name,omitempty"`
	Data      string  `json:"data"` // JSON string representing node details
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// ProxyPool represents a pool of proxies.
type ProxyPool struct {
	ID         string  `json:"id"`
	IsActive   int     `json:"isActive"` // 0 or 1
	TestStatus *string `json:"testStatus,omitempty"`
	Data       string  `json:"data"` // JSON string representing proxy credentials and checks
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

// Pseudo identity recorded for requests made from the admin dashboard, which
// authenticates with a session cookie rather than an API key.
const (
	AdminSessionKeyID   = "admin_session"
	AdminSessionKeyName = "Admin Dashboard Session"

	// Rows that carry no api_keys record still need a readable label in the
	// audit trail: a request sent with no key at all, and one whose key is no
	// longer present in api_keys.
	NoClientKeyID        = "none"
	NoClientKeyName      = "No API Key"
	UnknownClientKeyName = "Unknown API Key"
)

// APIKey represents a client-facing authorization key.
type APIKey struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        *string `json:"name,omitempty"`
	MachineID   *string `json:"machineId,omitempty"`
	IsActive    int     `json:"isActive"` // 0 or 1
	CreatedAt   string  `json:"createdAt"`
	QuotaLimit  int64   `json:"quotaLimit"`
	UsageTokens int64   `json:"usageTokens"`
	CreditLimit float64 `json:"creditLimit"`
	UsageCost   float64 `json:"usageCost"`
	RateLimit   int     `json:"rateLimit"`
	// AllowedModels is the per-key model scope ("Restricted" in the dashboard).
	// Empty means unrestricted. Enforced by ModelAllowed.
	AllowedModels []string `json:"allowed_models,omitempty"`
	// ScopeInvalid marks a stored scope that could not be parsed. Such a key is
	// treated as restricted-but-empty (deny every model) so a corrupt row can
	// never silently grant unlimited access.
	ScopeInvalid bool `json:"-"`
}

// Combo represents a multi-model routing combinated alias.
type Combo struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         *string `json:"kind,omitempty"`
	Models       string  `json:"models"`   // JSON string representing model selection details
	Strategy     string  `json:"strategy"` // routing strategy: "fallback", "round-robin", "capacity", "fusion"
	SystemPrompt string  `json:"systemPrompt,omitempty"` // injected before every request on this combo
	InjectMode   string  `json:"injectMode,omitempty"`   // "prepend" | "append" | "replace" | "hidden_user"
	// PromptInjectionEnabled is the stored flag. nil means the row predates the
	// flag, so prompt presence decides (see promptinject.ResolveEnabled).
	PromptInjectionEnabled *int64 `json:"promptInjectionEnabled,omitempty"`
	// DisplayIdentity is the value for {{display_name}}; empty falls back to Name.
	DisplayIdentity string `json:"displayIdentity,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// KV represents a scoped key-value store entry.
type KV struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// UsageHistory represents logged usage metrics for a request.
type UsageHistory struct {
	ID               int64   `json:"id"`
	Timestamp        string  `json:"timestamp"`
	Provider         *string `json:"provider,omitempty"`
	Model            *string `json:"model,omitempty"`
	ConnectionID     *string `json:"connectionId,omitempty"`
	APIKey           *string `json:"apiKey,omitempty"`
	Endpoint         *string `json:"endpoint,omitempty"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	Cost             float64 `json:"cost"`
	Status           *string `json:"status,omitempty"`
	Tokens           *string `json:"tokens,omitempty"` // JSON metadata on pricing / raw details
	Meta             *string `json:"meta,omitempty"`   // Extra metadata JSON
}

// UsageDaily represents aggregated usage per day.
type UsageDaily struct {
	DateKey string `json:"dateKey"`
	Data    string `json:"data"` // JSON string representing daily stats details
}

// RequestDetail represents the cached payload/response logs of requests.
type RequestDetail struct {
	ID           string  `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Provider     *string `json:"provider,omitempty"`
	Model        *string `json:"model,omitempty"`
	ConnectionID *string `json:"connectionId,omitempty"`
	Status       *string `json:"status,omitempty"`
	Data         string  `json:"data"` // JSON representation of request/response payload
}
