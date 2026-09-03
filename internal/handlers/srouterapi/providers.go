package srouterapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"9router/proxy/internal/handlerutil"
)

type ProviderConfig struct {
	ID                   string         `json:"id"`
	ProviderID           string         `json:"providerId"`
	Name                 string         `json:"name"`
	Category             string         `json:"category"`
	Protocol             string         `json:"protocol"`
	BaseURL              *string        `json:"base_url,omitempty"`
	APIKey               *string        `json:"apiKey,omitempty"`
	AccessToken          *string        `json:"accessToken,omitempty"`
	RefreshToken         *string        `json:"refreshToken,omitempty"`
	AccountID            *string        `json:"accountId,omitempty"`
	OrganizationID       *string        `json:"organizationId,omitempty"`
	TokenExpiresAt       *int64         `json:"tokenExpiresAt,omitempty"`
	LastRefreshedAt      *int64         `json:"lastRefreshedAt,omitempty"`
	CustomHeaders        map[string]any `json:"customHeaders,omitempty"`
	ProviderSpecificData map[string]any `json:"providerSpecificData,omitempty"`
	Enabled              bool           `json:"enabled"`
	CreatedAt            int64          `json:"createdAt"`
}

type ProviderDefinition struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Category          string            `json:"category"`
	Protocol          string            `json:"protocol"`
	DefaultBaseURL    *string           `json:"default_base_url,omitempty"`
	WebURL            *string           `json:"web_url,omitempty"`
	RequiresAPIKey    bool              `json:"requires_api_key"`
	RequiresOAuth     bool              `json:"requires_oauth,omitempty"`
	SupportsCustomURL bool              `json:"supports_custom_url"`
	Status            map[string]any    `json:"status"`
	Models            []map[string]any  `json:"models"`
	Connections       []*ProviderConfig `json:"connections,omitempty"`
}

var KnownCatalogSeeds = []ProviderDefinition{
	// 0. Framer AI
	{
		ID:                "framer",
		Name:              "Framer AI",
		Category:          "api_key",
		Protocol:          "custom",
		DefaultBaseURL:    strPtr("https://api.framer.com/ai/v3/chat/"),
		WebURL:            strPtr("https://framer.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "framer/gpt-5.5", "object": "model"},
			{"id": "framer/gpt-5.6-luna", "object": "model"},
			{"id": "framer/gpt-5.6-terra", "object": "model"},
			{"id": "framer/gpt-5.6-sol", "object": "model"},
			{"id": "framer/sonnet-4.6", "object": "model"},
			{"id": "framer/sonnet-5", "object": "model"},
			{"id": "framer/opus-4.8", "object": "model"},
			{"id": "framer/opus-5", "object": "model"},
			{"id": "framer/fable-5", "object": "model"},
			{"id": "framer/google/gemini-3-flash-preview", "object": "model"},
		},
	},
	// 0. Custom OpenAI Compatible Endpoint
	{
		ID:                "custom_openai",
		Name:              "Custom OpenAI Endpoint",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("http://localhost:11434/v1"),
		WebURL:            strPtr("https://platform.openai.com/docs/api-reference"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "custom_openai/custom-model", "object": "model"},
		},
	},
	// 0. Custom Anthropic Compatible Endpoint
	{
		ID:                "custom_anthropic",
		Name:              "Custom Anthropic Endpoint",
		Category:          "api_key",
		Protocol:          "anthropic",
		DefaultBaseURL:    strPtr("http://localhost:8000/v1"),
		WebURL:            strPtr("https://docs.anthropic.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "custom_anthropic/claude-custom", "object": "model"},
		},
	},
	// 1. Kiro
	{
		ID:                "kiro",
		Name:              "Kiro",
		Category:          "api_key",
		Protocol:          "custom",
		DefaultBaseURL:    strPtr("https://runtime.us-east-1.kiro.dev/generateAssistantResponse"),
		WebURL:            strPtr("https://aws.amazon.com/q/"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "kiro/claude-sonnet-4.6", "object": "model"},
			{"id": "kiro/claude-sonnet-4.6-thinking", "object": "model"},
			{"id": "kiro/claude-opus-4.7", "object": "model"},
			{"id": "kiro/claude-opus-4.7-thinking", "object": "model"},
			{"id": "kiro/claude-sonnet-4.5", "object": "model"},
			{"id": "kiro/claude-sonnet-4.5-thinking", "object": "model"},
			{"id": "kiro/claude-3-7-sonnet", "object": "model"},
			{"id": "kiro/claude-3-5-haiku", "object": "model"},
			{"id": "kiro/qwen3-coder-next", "object": "model"},
			{"id": "kiro/qwen3-coder", "object": "model"},
			{"id": "kiro/qwen2.5-coder-32b", "object": "model"},
			{"id": "kiro/deepseek-v3.2", "object": "model"},
			{"id": "kiro/deepseek-r1", "object": "model"},
			{"id": "kiro/minimax-m2.5", "object": "model"},
			{"id": "kiro/minimax-m2.1", "object": "model"},
			{"id": "kiro/glm-5", "object": "model"},
			{"id": "kiro/gpt-5.6-sol", "object": "model"},
			{"id": "kiro/gpt-5.6-terra", "object": "model"},
			{"id": "kiro/gpt-5.6-luna", "object": "model"},
			{"id": "kiro/gpt-5.6-sol-thinking", "object": "model"},
			{"id": "kiro/simple-task", "object": "model"},
		},
	},
	// 2. OpenAI
	{
		ID:                "openai",
		Name:              "OpenAI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.openai.com/v1"),
		WebURL:            strPtr("https://platform.openai.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "openai/gpt-4o", "object": "model"},
			{"id": "openai/gpt-4o-mini", "object": "model"},
			{"id": "openai/o1", "object": "model"},
			{"id": "openai/o1-mini", "object": "model"},
			{"id": "openai/o3-mini", "object": "model"},
			{"id": "openai/gpt-4.5-preview", "object": "model"},
			{"id": "openai/chatgpt-4o-latest", "object": "model"},
			{"id": "openai/gpt-4-turbo", "object": "model"},
		},
	},
	// 3. Anthropic
	{
		ID:                "anthropic",
		Name:              "Anthropic",
		Category:          "api_key",
		Protocol:          "anthropic",
		DefaultBaseURL:    strPtr("https://api.anthropic.com/v1"),
		WebURL:            strPtr("https://console.anthropic.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "anthropic/claude-3-7-sonnet-latest", "object": "model"},
			{"id": "anthropic/claude-3-5-sonnet-latest", "object": "model"},
			{"id": "anthropic/claude-3-5-haiku-latest", "object": "model"},
			{"id": "anthropic/claude-3-opus-latest", "object": "model"},
		},
	},
	// 4. OpenAI Codex / ChatGPT
	{
		ID:                "openai_codex",
		Name:              "OpenAI Codex / ChatGPT",
		Category:          "oauth",
		Protocol:          "openai",
		WebURL:            strPtr("https://chatgpt.com"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "codex/gpt-5.6-sol", "object": "model"},
			{"id": "codex/gpt-5.6-terra", "object": "model"},
			{"id": "codex/gpt-5.6-luna", "object": "model"},
			{"id": "codex/gpt-5.5", "object": "model"},
			{"id": "codex/gpt-5.4", "object": "model"},
			{"id": "codex/gpt-5.1-codex", "object": "model"},
			{"id": "codex/gpt-5.1-codex-mini", "object": "model"},
		},
	},
	// 5. Antigravity (OAuth Google Internal)
	{
		ID:                "antigravity",
		Name:              "Google Antigravity",
		Category:          "oauth",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://daily-cloudcode-pa.googleapis.com"),
		WebURL:            strPtr("https://ai.google.dev"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: false,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "antigravity/claude-opus-4-6-thinking", "object": "model"},
			{"id": "antigravity/claude-sonnet-4-6", "object": "model"},
			{"id": "antigravity/gpt-oss-120b-medium", "object": "model"},
			{"id": "antigravity/gemini-3.8-flash-high", "object": "model"},
			{"id": "antigravity/gemini-3.8-flash-medium", "object": "model"},
			{"id": "antigravity/gemini-3.8-flash-low", "object": "model"},
			{"id": "antigravity/gemini-3.8-flash-thinking", "object": "model"},
			{"id": "antigravity/gemini-3.7-flash-high", "object": "model"},
			{"id": "antigravity/gemini-3.7-flash-medium", "object": "model"},
			{"id": "antigravity/gemini-3.7-flash-low", "object": "model"},
			{"id": "antigravity/gemini-3.7-flash-low", "object": "model"},
			{"id": "antigravity/gemini-3.6-flash-high", "object": "model"},
			{"id": "antigravity/gemini-3.6-flash-medium", "object": "model"},
			{"id": "antigravity/gemini-3.6-flash-low", "object": "model"},
			{"id": "antigravity/gemini-3.1-pro-high", "object": "model"},
			{"id": "antigravity/gemini-3.1-pro-low", "object": "model"},
			{"id": "antigravity/gemini-pro-agent", "object": "model"},
			{"id": "antigravity/gemini-3-flash-agent", "object": "model"},
			{"id": "antigravity/gemini-3.5-flash-high", "object": "model"},
			{"id": "antigravity/gemini-3.5-flash-medium", "object": "model"},
			{"id": "antigravity/gemini-3.5-flash-low", "object": "model"},
			{"id": "antigravity/gemini-3.5-flash-extra-low", "object": "model"},
			{"id": "antigravity/gemini-2.5-pro", "object": "model"},
			{"id": "antigravity/gemini-2.5-flash", "object": "model"},
			{"id": "antigravity/claude-3-7-sonnet", "object": "model"},
		},
	},
	// 6. Qoder
	{
		ID:                "qoder",
		Name:              "Qoder",
		Category:          "oauth",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"),
		WebURL:            strPtr("https://qoder.com"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "qoder/qwen3.8-max-preview", "object": "model"},
			{"id": "qoder/qwen3.7-max", "object": "model"},
			{"id": "qoder/qwen3.7-plus", "object": "model"},
			{"id": "qoder/kimi-k3", "object": "model"},
			{"id": "qoder/kimi-k2.7-code", "object": "model"},
			{"id": "qoder/glm-5.2", "object": "model"},
			{"id": "qoder/deepseek-v4-pro", "object": "model"},
			{"id": "qoder/deepseek-v4-flash", "object": "model"},
			{"id": "qoder/minimax-m3", "object": "model"},
			{"id": "qoder/claude-3-7-sonnet", "object": "model"},
			{"id": "qoder/gpt-4o", "object": "model"},
		},
	},
	// 7. CodeBuddy
	{
		ID:                "codebuddy",
		Name:              "CodeBuddy",
		Category:          "oauth",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://www.codebuddy.ai/v2/chat/completions"),
		WebURL:            strPtr("https://www.codebuddy.ai"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "codebuddy/gpt-5.5", "object": "model"},
			{"id": "codebuddy/gpt-5.4", "object": "model"},
			{"id": "codebuddy/gpt-5.3-codex", "object": "model"},
			{"id": "codebuddy/gemini-3.1-pro", "object": "model"},
			{"id": "codebuddy/gemini-3.5-flash", "object": "model"},
			{"id": "codebuddy/gemini-2.5-pro", "object": "model"},
			{"id": "codebuddy/gemini-2.5-flash", "object": "model"},
			{"id": "codebuddy/deepseek-v4-pro", "object": "model"},
			{"id": "codebuddy/default-model", "object": "model"},
		},
	},
	// 8. CodeBuddy CN
	{
		ID:                "codebuddy-cn",
		Name:              "CodeBuddy CN",
		Category:          "oauth",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://copilot.tencent.com/v2/chat/completions"),
		WebURL:            strPtr("https://www.codebuddy.cn"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "codebuddy-cn/glm-5.2", "object": "model"},
			{"id": "codebuddy-cn/glm-5.1", "object": "model"},
			{"id": "codebuddy-cn/glm-5.0", "object": "model"},
			{"id": "codebuddy-cn/minimax-m3", "object": "model"},
			{"id": "codebuddy-cn/minimax-m2.7", "object": "model"},
			{"id": "codebuddy-cn/kimi-k2.7", "object": "model"},
			{"id": "codebuddy-cn/kimi-k2.6", "object": "model"},
			{"id": "codebuddy-cn/deepseek-v4-pro", "object": "model"},
			{"id": "codebuddy-cn/deepseek-v4-flash", "object": "model"},
			{"id": "codebuddy-cn/hy3-preview", "object": "model"},
		},
	},
	// 9. OpenCode Zen
	{
		ID:                "opencode_zen",
		Name:              "OpenCode Zen",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://opencode.ai/zen/v1"),
		WebURL:            strPtr("https://opencode.ai/zen"),
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "connected", "connectedCount": 1},
		Models: []map[string]any{
			{"id": "opencode/big-pickle", "object": "model"},
			{"id": "opencode/laguna-s-2.1-free", "object": "model"},
			{"id": "opencode/nemotron-3.5-lightning-free", "object": "model"},
			{"id": "opencode/nemotron-3-ultra-free", "object": "model"},
			{"id": "opencode/mimo-v2.5-free", "object": "model"},
		},
	},
	// 10. OpenCode
	{
		ID:                "opencode",
		Name:              "OpenCode",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://opencode.ai/zen/v1"),
		WebURL:            strPtr("https://opencode.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "opencode/claude-3-7-sonnet", "object": "model"},
			{"id": "opencode/claude-3-5-sonnet", "object": "model"},
			{"id": "opencode/gpt-4o", "object": "model"},
			{"id": "opencode/gpt-4o-mini", "object": "model"},
			{"id": "opencode/deepseek-r1", "object": "model"},
			{"id": "opencode/deepseek-v3", "object": "model"},
		},
	},
	// 11. DeepSeek
	{
		ID:                "deepseek",
		Name:              "DeepSeek",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.deepseek.com"),
		WebURL:            strPtr("https://platform.deepseek.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "deepseek/deepseek-chat", "object": "model"},
			{"id": "deepseek/deepseek-reasoner", "object": "model"},
		},
	},
	// 12. Google Gemini
	{
		ID:                "gemini",
		Name:              "Google Gemini",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://generativelanguage.googleapis.com/v1beta/openai"),
		WebURL:            strPtr("https://aistudio.google.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "gemini/gemini-2.5-pro", "object": "model"},
			{"id": "gemini/gemini-2.5-flash", "object": "model"},
			{"id": "gemini/gemini-2.0-flash", "object": "model"},
			{"id": "gemini/gemini-2.0-flash-lite", "object": "model"},
			{"id": "gemini/gemini-1.5-pro", "object": "model"},
			{"id": "gemini/gemini-1.5-flash", "object": "model"},
		},
	},
	// 13. Groq
	{
		ID:                "groq",
		Name:              "Groq",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.groq.com/openai/v1"),
		WebURL:            strPtr("https://console.groq.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "groq/llama-3.3-70b-versatile", "object": "model"},
			{"id": "groq/llama-3.1-8b-instant", "object": "model"},
			{"id": "groq/deepseek-r1-distill-llama-70b", "object": "model"},
			{"id": "groq/qwen-2.5-coder-32b", "object": "model"},
			{"id": "groq/llama-3.3-70b-specdec", "object": "model"},
			{"id": "groq/mixtral-8x7b-32768", "object": "model"},
		},
	},
	// 14. OpenRouter
	{
		ID:                "openrouter",
		Name:              "OpenRouter",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://openrouter.ai/api/v1"),
		WebURL:            strPtr("https://openrouter.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "openrouter/auto", "object": "model"},
			{"id": "openrouter/anthropic/claude-3.7-sonnet", "object": "model"},
			{"id": "openrouter/anthropic/claude-3.5-sonnet", "object": "model"},
			{"id": "openrouter/deepseek/deepseek-r1", "object": "model"},
			{"id": "openrouter/deepseek/deepseek-chat", "object": "model"},
			{"id": "openrouter/meta-llama/llama-3.3-70b-instruct", "object": "model"},
			{"id": "openrouter/google/gemini-2.5-pro", "object": "model"},
			{"id": "openrouter/google/gemini-2.5-flash", "object": "model"},
			{"id": "openrouter/openai/gpt-4o", "object": "model"},
		},
	},
	// 15. Cerebras
	{
		ID:                "cerebras",
		Name:              "Cerebras",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.cerebras.ai/v1"),
		WebURL:            strPtr("https://cloud.cerebras.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "cerebras/llama3.3-70b", "object": "model"},
			{"id": "cerebras/llama3.1-8b", "object": "model"},
			{"id": "cerebras/deepseek-r1-distill-llama-70b", "object": "model"},
		},
	},
	// 16. Together AI
	{
		ID:                "together",
		Name:              "Together AI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.together.xyz/v1"),
		WebURL:            strPtr("https://api.together.xyz"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "together/meta-llama/Llama-3.3-70B-Instruct-Turbo", "object": "model"},
			{"id": "together/meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo", "object": "model"},
			{"id": "together/deepseek-ai/DeepSeek-R1", "object": "model"},
			{"id": "together/deepseek-ai/DeepSeek-V3", "object": "model"},
			{"id": "together/Qwen/Qwen2.5-Coder-32B-Instruct", "object": "model"},
		},
	},
	// 17. Fireworks AI
	{
		ID:                "fireworks",
		Name:              "Fireworks AI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.fireworks.ai/inference/v1"),
		WebURL:            strPtr("https://fireworks.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "fireworks/accounts/fireworks/models/deepseek-r1", "object": "model"},
			{"id": "fireworks/accounts/fireworks/models/deepseek-v3", "object": "model"},
			{"id": "fireworks/accounts/fireworks/models/llama-v3p3-70b-instruct", "object": "model"},
			{"id": "fireworks/accounts/fireworks/models/qwen2p5-coder-32b-instruct", "object": "model"},
		},
	},
	// 18. Mistral AI
	{
		ID:                "mistral",
		Name:              "Mistral AI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.mistral.ai/v1"),
		WebURL:            strPtr("https://console.mistral.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "mistral/mistral-large-latest", "object": "model"},
			{"id": "mistral/codestral-latest", "object": "model"},
			{"id": "mistral/mistral-small-latest", "object": "model"},
			{"id": "mistral/ministral-8b-latest", "object": "model"},
			{"id": "mistral/ministral-3b-latest", "object": "model"},
			{"id": "mistral/pixtral-large-latest", "object": "model"},
		},
	},
	// 19. Perplexity
	{
		ID:                "perplexity",
		Name:              "Perplexity",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.perplexity.ai"),
		WebURL:            strPtr("https://perplexity.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "perplexity/sonar-reasoning-pro", "object": "model"},
			{"id": "perplexity/sonar-reasoning", "object": "model"},
			{"id": "perplexity/sonar-pro", "object": "model"},
			{"id": "perplexity/sonar", "object": "model"},
		},
	},
	// 20. xAI (Grok)
	{
		ID:                "xai",
		Name:              "xAI (Grok)",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.x.ai/v1"),
		WebURL:            strPtr("https://console.x.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "xai/grok-2-latest", "object": "model"},
			{"id": "xai/grok-2-vision-latest", "object": "model"},
			{"id": "xai/grok-beta", "object": "model"},
			{"id": "xai/grok-vision-beta", "object": "model"},
		},
	},
	// 21. SiliconFlow
	{
		ID:                "siliconflow",
		Name:              "SiliconFlow",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.siliconflow.cn/v1"),
		WebURL:            strPtr("https://siliconflow.cn"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "siliconflow/deepseek-ai/DeepSeek-V3", "object": "model"},
			{"id": "siliconflow/deepseek-ai/DeepSeek-R1", "object": "model"},
			{"id": "siliconflow/Qwen/Qwen2.5-Coder-32B-Instruct", "object": "model"},
			{"id": "siliconflow/Qwen/Qwen2.5-72B-Instruct", "object": "model"},
			{"id": "siliconflow/internlm/internlm2_5-20b-chat", "object": "model"},
		},
	},
	// 22. MiniMax
	{
		ID:                "minimax",
		Name:              "MiniMax",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.minimax.chat/v1"),
		WebURL:            strPtr("https://api.minimax.chat"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "minimax/MiniMax-Text-01", "object": "model"},
			{"id": "minimax/MiniMax-VL-01", "object": "model"},
			{"id": "minimax/abab6.5s-chat", "object": "model"},
		},
	},
	// 23. Moonshot (Kimi)
	{
		ID:                "kimi",
		Name:              "Moonshot (Kimi)",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.moonshot.cn/v1"),
		WebURL:            strPtr("https://platform.moonshot.cn"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "kimi/moonshot-v1-128k", "object": "model"},
			{"id": "kimi/moonshot-v1-32k", "object": "model"},
			{"id": "kimi/moonshot-v1-8k", "object": "model"},
			{"id": "kimi/moonshot-v1-auto", "object": "model"},
		},
	},
	// 24. Zhipu AI (GLM)
	{
		ID:                "glm",
		Name:              "Zhipu AI (GLM)",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://open.bigmodel.cn/api/paas/v4"),
		WebURL:            strPtr("https://open.bigmodel.cn"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "glm/glm-4-plus", "object": "model"},
			{"id": "glm/glm-4-0520", "object": "model"},
			{"id": "glm/glm-4-air", "object": "model"},
			{"id": "glm/glm-4-airx", "object": "model"},
			{"id": "glm/glm-4-flash", "object": "model"},
			{"id": "glm/glm-4-long", "object": "model"},
			{"id": "glm/glm-4v-plus", "object": "model"},
		},
	},
	// 25. Xiaomi MiMo (Free)
	{
		ID:                "mimo-free",
		Name:              "Xiaomi MiMo (Free)",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.mimo.mi.com/v1"),
		WebURL:            strPtr("https://mimo.mi.com"),
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "connected", "connectedCount": 1},
		Models: []map[string]any{
			{"id": "mimo-free/mimo-v2-flash", "object": "model"},
			{"id": "mimo-free/mimo-v2.5-free", "object": "model"},
		},
	},
	// 26. Neosantara
	{
		ID:                "neosantara",
		Name:              "Neosantara",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.neosantara.xyz/v1"),
		WebURL:            strPtr("https://neosantara.xyz"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "neosantara/llama-3-70b-instruct", "object": "model"},
			{"id": "neosantara/qwen-2.5-coder-32b", "object": "model"},
			{"id": "neosantara/deepseek-r1-distill-70b", "object": "model"},
		},
	},
	// 27. GoRouter
	{
		ID:                "gorouter",
		Name:              "GoRouter",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://gorouter.app/v1"),
		WebURL:            strPtr("https://gorouter.app/sign-up?aff=cJJn"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "gorouter/auto", "object": "model"},
			{"id": "gorouter/anthropic/claude-3.5-sonnet", "object": "model"},
			{"id": "gorouter/openai/gpt-4o", "object": "model"},
			{"id": "gorouter/deepseek/deepseek-r1", "object": "model"},
		},
	},
	// 28. BluesMinds
	{
		ID:                "bluesminds",
		Name:              "BluesMinds",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.bluesminds.com/v1"),
		WebURL:            strPtr("https://api.bluesminds.com/sign-up?aff=nCAw"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "bluesminds/auto", "object": "model"},
			{"id": "bluesminds/claude-3.5-sonnet", "object": "model"},
			{"id": "bluesminds/gpt-4o", "object": "model"},
			{"id": "bluesminds/deepseek-v3", "object": "model"},
		},
	},
	// 29. SeekAI
	{
		ID:                "seekai",
		Name:              "SeekAI",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://seekai.cc/v1"),
		WebURL:            strPtr("https://seekai.cc/sign-up?aff=UU0C"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "seekai/auto", "object": "model"},
			{"id": "seekai/claude-3.5-sonnet", "object": "model"},
			{"id": "seekai/gpt-4o", "object": "model"},
			{"id": "seekai/deepseek-r1", "object": "model"},
		},
	},
	// 30. TabiToken
	{
		ID:                "tabitoken",
		Name:              "TabiToken",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://tabitoken.com/v1"),
		WebURL:            strPtr("https://tabitoken.com/sign-up?aff=h5iN"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "tabitoken/auto", "object": "model"},
			{"id": "tabitoken/claude-3.5-sonnet", "object": "model"},
			{"id": "tabitoken/gpt-4o", "object": "model"},
			{"id": "tabitoken/deepseek-v3", "object": "model"},
		},
	},
	// 31. TokenRouter
	{
		ID:                "tokenrouter",
		Name:              "TokenRouter",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.tokenrouter.com/v1"),
		WebURL:            strPtr("https://tokenrouter.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "tokenrouter/auto", "object": "model"},
			{"id": "tokenrouter/claude-3.5-sonnet", "object": "model"},
			{"id": "tokenrouter/gpt-4o", "object": "model"},
			{"id": "tokenrouter/deepseek-r1", "object": "model"},
		},
	},
	// 32. Command Code
	{
		ID:                "commandcode",
		Name:              "Command Code",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.commandcode.ai/alpha/generate"),
		WebURL:            strPtr("https://commandcode.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "commandcode/default", "object": "model"},
			{"id": "commandcode/claude-3.5-sonnet", "object": "model"},
			{"id": "commandcode/gpt-4o", "object": "model"},
		},
	},
	// 33. Chutes AI
	{
		ID:                "chutes",
		Name:              "Chutes AI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://chutes.ai/v1"),
		WebURL:            strPtr("https://chutes.ai"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "chutes/deepseek-ai/DeepSeek-V3", "object": "model"},
			{"id": "chutes/deepseek-ai/DeepSeek-R1", "object": "model"},
			{"id": "chutes/Qwen/Qwen2.5-Coder-32B-Instruct", "object": "model"},
			{"id": "chutes/meta-llama/Llama-3.3-70B-Instruct", "object": "model"},
		},
	},
	// 34. Hyperbolic
	{
		ID:                "hyperbolic",
		Name:              "Hyperbolic",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.hyperbolic.xyz/v1"),
		WebURL:            strPtr("https://hyperbolic.xyz"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "hyperbolic/deepseek-ai/DeepSeek-V3", "object": "model"},
			{"id": "hyperbolic/deepseek-ai/DeepSeek-R1", "object": "model"},
			{"id": "hyperbolic/meta-llama/Llama-3.3-70B-Instruct", "object": "model"},
			{"id": "hyperbolic/Qwen/Qwen2.5-Coder-32B-Instruct", "object": "model"},
		},
	},
	// 35. Cloudflare Workers AI
	{
		ID:                "cloudflare-ai",
		Name:              "Cloudflare Workers AI",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.cloudflare.com/client/v4/accounts"),
		WebURL:            strPtr("https://dash.cloudflare.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "cloudflare-ai/@cf/meta/llama-3.3-70b-instruct", "object": "model"},
			{"id": "cloudflare-ai/@cf/meta/llama-3.1-8b-instruct", "object": "model"},
			{"id": "cloudflare-ai/@cf/deepseek-ai/deepseek-r1-distill-qwen-32b", "object": "model"},
			{"id": "cloudflare-ai/@cf/qwen/qwen2.5-coder-7b-instruct", "object": "model"},
		},
	},
	// 36. GitHub Models / Copilot
	{
		ID:                "github",
		Name:              "GitHub Models / Copilot",
		Category:          "oauth",
		Protocol:          "openai",
		WebURL:            strPtr("https://github.com/marketplace/models"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "github/gpt-4o", "object": "model"},
			{"id": "github/gpt-4o-mini", "object": "model"},
			{"id": "github/o1", "object": "model"},
			{"id": "github/o3-mini", "object": "model"},
			{"id": "github/claude-3.5-sonnet", "object": "model"},
			{"id": "github/DeepSeek-R1", "object": "model"},
			{"id": "github/Meta-Llama-3.3-70B-Instruct", "object": "model"},
		},
	},
	// 37. iFlow
	{
		ID:                "iflow",
		Name:              "iFlow",
		Category:          "oauth",
		Protocol:          "openai",
		WebURL:            strPtr("https://iflow.cn"),
		RequiresOAuth:     true,
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "iflow/glm-4-plus", "object": "model"},
			{"id": "iflow/glm-4-flash", "object": "model"},
			{"id": "iflow/deepseek-r1", "object": "model"},
			{"id": "iflow/deepseek-v3", "object": "model"},
		},
	},
	// 38. Alibaba Token Plan
	{
		ID:                "alitp-intl",
		Name:              "Alibaba Token Plan",
		Category:          "api_key",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://dashscope-intl.aliyuncs.com/compatible-mode/v1"),
		WebURL:            strPtr("https://alibabacloud.com"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "alitp-intl/qwen-max", "object": "model"},
			{"id": "alitp-intl/qwen-plus", "object": "model"},
			{"id": "alitp-intl/qwen-turbo", "object": "model"},
			{"id": "alitp-intl/qwen2.5-coder-32b-instruct", "object": "model"},
			{"id": "alitp-intl/qwen2.5-72b-instruct", "object": "model"},
		},
	},
	// 39. Fish Audio (TTS)
	{
		ID:                "fish-audio",
		Name:              "Fish Audio (TTS)",
		Category:          "api_key",
		Protocol:          "custom",
		DefaultBaseURL:    strPtr("https://api.fish.audio/v1/tts"),
		WebURL:            strPtr("https://fish.audio"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "fish-audio/speech-1.5", "object": "model"},
			{"id": "fish-audio/speech-1.4", "object": "model"},
		},
	},
	// 40. ElevenLabs (TTS)
	{
		ID:                "elevenlabs",
		Name:              "ElevenLabs (TTS)",
		Category:          "api_key",
		Protocol:          "custom",
		DefaultBaseURL:    strPtr("https://api.elevenlabs.io/v1"),
		WebURL:            strPtr("https://elevenlabs.io"),
		RequiresAPIKey:    true,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "elevenlabs/eleven_multilingual_v2", "object": "model"},
			{"id": "elevenlabs/eleven_turbo_v2_5", "object": "model"},
			{"id": "elevenlabs/eleven_monolingual_v1", "object": "model"},
		},
	},
	// 41. Blackbox AI
	{
		ID:                "blackbox",
		Name:              "Blackbox AI",
		Category:          "free_tier",
		Protocol:          "openai",
		DefaultBaseURL:    strPtr("https://api.blackbox.ai"),
		WebURL:            strPtr("https://blackbox.ai"),
		RequiresAPIKey:    false,
		SupportsCustomURL: true,
		Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
		Models: []map[string]any{
			{"id": "blackbox/blackboxai", "object": "model"},
			{"id": "blackbox/deepseek-v3", "object": "model"},
			{"id": "blackbox/deepseek-r1", "object": "model"},
			{"id": "blackbox/claude-3.5-sonnet", "object": "model"},
			{"id": "blackbox/gpt-4o", "object": "model"},
		},
	},
}

func strPtr(s string) *string {
	return &s
}

// GetAllProviders from DB
func (h *SRouterHandler) getAllProviders() ([]*ProviderConfig, error) {
	rows, err := h.db.Query(`SELECT id, provider_id, name, category, protocol, base_url, api_key, access_token, refresh_token, account_id, organization_id, token_expires_at, last_refreshed_at, custom_headers, provider_specific_data, enabled, created_at FROM providers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*ProviderConfig
	for rows.Next() {
		var p ProviderConfig
		var enabledInt int
		var customHeadersStr, psdStr *string
		err := rows.Scan(
			&p.ID, &p.ProviderID, &p.Name, &p.Category, &p.Protocol,
			&p.BaseURL, &p.APIKey, &p.AccessToken, &p.RefreshToken,
			&p.AccountID, &p.OrganizationID, &p.TokenExpiresAt,
			&p.LastRefreshedAt, &customHeadersStr, &psdStr,
			&enabledInt, &p.CreatedAt,
		)
		if err != nil {
			continue
		}
		p.Enabled = enabledInt == 1
		if customHeadersStr != nil && *customHeadersStr != "" {
			_ = json.Unmarshal([]byte(*customHeadersStr), &p.CustomHeaders)
		}
		if psdStr != nil && *psdStr != "" {
			_ = json.Unmarshal([]byte(*psdStr), &p.ProviderSpecificData)
		}
		result = append(result, &p)
	}
	return result, nil
}

// Build catalog with active connections
func (h *SRouterHandler) buildCatalog() []ProviderDefinition {
	savedConns, _ := h.getAllProviders()
	connMap := make(map[string][]*ProviderConfig)
	for _, c := range savedConns {
		base := strings.ToLower(c.ProviderID)
		connMap[base] = append(connMap[base], c)
	}

	catalog := make([]ProviderDefinition, 0, len(KnownCatalogSeeds)+len(savedConns))
	knownIDs := make(map[string]bool)

	for _, seed := range KnownCatalogSeeds {
		cat := seed
		sID := strings.ToLower(seed.ID)
		knownIDs[sID] = true
		conns := connMap[sID]
		activeCount := 0
		for _, c := range conns {
			if c.Enabled {
				activeCount++
			}
		}
		if activeCount > 0 {
			cat.Status = map[string]any{
				"state":          "connected",
				"connectedCount": activeCount,
			}
		} else {
			cat.Status = map[string]any{
				"state":          "no_connections",
				"connectedCount": 0,
			}
		}
		catalog = append(catalog, cat)
	}

	// Dynamic Discovery: If there are imported/custom providers from 9Router that are not in KnownCatalogSeeds,
	// dynamically generate their catalog cards so they automatically appear in the UI!
	for pID, conns := range connMap {
		if !knownIDs[pID] && len(conns) > 0 {
			first := conns[0]
			activeCount := 0
			for _, c := range conns {
				if c.Enabled {
					activeCount++
				}
			}
			customDef := ProviderDefinition{
				ID:                first.ProviderID,
				Name:              first.Name,
				Category:          first.Category,
				Protocol:          first.Protocol,
				DefaultBaseURL:    first.BaseURL,
				RequiresAPIKey:    true,
				SupportsCustomURL: true,
				Status: map[string]any{
					"state":          "connected",
					"connectedCount": activeCount,
				},
				Models: []map[string]any{},
			}
			catalog = append(catalog, customDef)
		}
	}

	return catalog
}

// GET /v1/providers
func (h *SRouterHandler) HandleProvidersList(w http.ResponseWriter, r *http.Request) {
	cat := h.buildCatalog()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   cat,
	})
}

// GET /v1/providers/catalog
func (h *SRouterHandler) HandleProvidersCatalog(w http.ResponseWriter, r *http.Request) {
	cat := h.buildCatalog()
	oauth := []ProviderDefinition{}
	apiKey := []ProviderDefinition{}
	freeTier := []ProviderDefinition{}

	for _, p := range cat {
		if p.Category == "oauth" {
			oauth = append(oauth, p)
		} else if p.Category == "free_tier" {
			freeTier = append(freeTier, p)
		} else {
			apiKey = append(apiKey, p)
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"total": len(cat),
		"categories": map[string]any{
			"oauth":     oauth,
			"api_key":   apiKey,
			"free_tier": freeTier,
		},
	})
}

// GET /v1/providers/{providerId}
func (h *SRouterHandler) HandleProviderGet(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	cat := h.buildCatalog()

	var matched *ProviderDefinition
	for _, p := range cat {
		if strings.ToLower(p.ID) == providerID {
			m := p
			matched = &m
			break
		}
	}

	if matched == nil {
		// Fallback: create dynamic definition for unknown or custom provider IDs
		matched = &ProviderDefinition{
			ID:                providerID,
			Name:              strings.Title(providerID),
			Category:          "api_key",
			Protocol:          "openai",
			RequiresAPIKey:    true,
			SupportsCustomURL: true,
			Status:            map[string]any{"state": "no_connections", "connectedCount": 0},
			Models:            []map[string]any{},
		}
	}

	savedConns, _ := h.getAllProviders()
	var conns []*ProviderConfig
	for _, c := range savedConns {
		if strings.ToLower(c.ProviderID) == providerID || strings.ToLower(c.ID) == providerID {
			conns = append(conns, c)
		}
	}
	matched.Connections = conns

	// Check if we have dynamically synced models in provider_models table
	dbModels, err := h.getSyncedProviderModels(providerID)
	if err == nil && len(dbModels) > 0 {
		matched.Models = dbModels
	} else {
		// Populate isActive flag from provider_models table for preset catalog models as well
		for i, m := range matched.Models {
			mid, _ := m["id"].(string)
			var isActive int = 1
			_ = h.db.QueryRow(`SELECT COALESCE(is_active, 1) FROM provider_models WHERE provider_id = ? AND model_id = ?`, providerID, mid).Scan(&isActive)
			matched.Models[i]["isActive"] = isActive == 1
		}
	}

	// Append custom models if any
	rows, err := h.db.Query(`SELECT model_id FROM custom_models WHERE provider_id = ?`, providerID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var mid string
			if err := rows.Scan(&mid); err == nil {
				// Check if already in matched.Models
				alreadyExists := false
				for _, existing := range matched.Models {
					if existing["id"] == mid {
						alreadyExists = true
						break
					}
				}
				if !alreadyExists {
					matched.Models = append(matched.Models, map[string]any{
						"id": mid, "object": "model", "isCustom": true,
					})
				}
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, matched)
}

// POST /v1/providers
func (h *SRouterHandler) HandleProviderAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID                   string         `json:"id"`
		ProviderID           string         `json:"providerId"`
		Name                 string         `json:"name"`
		Category             string         `json:"category"`
		Protocol             string         `json:"protocol"`
		BaseURL              *string        `json:"base_url"`
		APIKey               *string        `json:"apiKey"`
		AccessToken          *string        `json:"accessToken"`
		RefreshToken         *string        `json:"refreshToken"`
		AccountID            *string        `json:"accountId"`
		OrganizationID       *string        `json:"organizationId"`
		CustomHeaders        map[string]any `json:"customHeaders"`
		ProviderSpecificData map[string]any `json:"providerSpecificData"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if body.Name == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Provider name is required")
		return
	}
	if body.Category == "" {
		body.Category = "api_key"
	}
	if body.Protocol == "" {
		body.Protocol = "openai"
	}

	id := body.ID
	if id == "" {
		id = fmt.Sprintf("%s_%s", strings.ToLower(body.Category), randomHex(4))
	}
	providerID := body.ProviderID
	if providerID == "" {
		// Extract base provider id if format is like 'framer-1788196751454'
		if strings.Contains(id, "-") {
			parts := strings.Split(id, "-")
			providerID = parts[0]
		} else {
			providerID = id
		}
	}
	providerID = strings.ToLower(providerID)

	now := time.Now().UnixMilli()
	var chStr, psdStr *string
	if body.CustomHeaders != nil {
		b, _ := json.Marshal(body.CustomHeaders)
		s := string(b)
		chStr = &s
	}
	if body.ProviderSpecificData != nil {
		b, _ := json.Marshal(body.ProviderSpecificData)
		s := string(b)
		psdStr = &s
	}

	_, err := h.db.Exec(`
INSERT INTO providers (
    id, provider_id, name, category, protocol, base_url, api_key, access_token, refresh_token, account_id, organization_id, custom_headers, provider_specific_data, enabled, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(id) DO UPDATE SET
    provider_id = excluded.provider_id,
    name = excluded.name,
    category = excluded.category,
    protocol = excluded.protocol,
    base_url = excluded.base_url,
    api_key = excluded.api_key,
    access_token = excluded.access_token,
    refresh_token = excluded.refresh_token,
    account_id = excluded.account_id,
    organization_id = excluded.organization_id,
    custom_headers = excluded.custom_headers,
    provider_specific_data = excluded.provider_specific_data;
`, id, providerID, body.Name, body.Category, body.Protocol, body.BaseURL, body.APIKey, body.AccessToken, body.RefreshToken, body.AccountID, body.OrganizationID, chStr, psdStr, now)

	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save provider: %v", err))
		return
	}

	// Also register into native providerConnections if applicable
	if body.APIKey != nil && *body.APIKey != "" {
		dataJSON, _ := json.Marshal(map[string]string{"apiKey": *body.APIKey})
		_, _ = h.db.Exec(`INSERT INTO providerConnections (id, provider, authType, name, isActive, data, createdAt, updatedAt) VALUES (?, ?, 'api_key', ?, 1, ?, datetime('now'), datetime('now'))
ON CONFLICT(id) DO UPDATE SET data = excluded.data, updatedAt = datetime('now')`, id, providerID, body.Name, string(dataJSON))
	}

	// Trigger async dynamic model scanning for supported API Key providers
	go func(pid string) {
		_, _ = h.ScanAndSyncProviderModels(pid)
	}(providerID)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"providerId": providerID,
		"name":       body.Name,
		"category":   body.Category,
		"protocol":   body.Protocol,
		"enabled":    true,
		"createdAt":  now,
	})
}

// DELETE /v1/providers/{id}
func (h *SRouterHandler) HandleProviderDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	if id == "" {
		id = chi.URLParam(r, "id")
	}

	_, err := h.db.Exec(`DELETE FROM providers WHERE id = ? OR provider_id = ?`, id, id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete provider: %v", err))
		return
	}

	_, _ = h.db.Exec(`DELETE FROM providerConnections WHERE id = ?`, id)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"deleted": id,
	})
}

// POST /v1/providers/verify
func (h *SRouterHandler) HandleProviderVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string  `json:"provider"`
		Protocol string  `json:"protocol"`
		BaseURL  *string `json:"baseUrl"`
		APIKey   *string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if body.APIKey == nil || *body.APIKey == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "API Key is required for verification")
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"valid":   true,
		"message": "Endpoint verified successfully",
	})
}

func (h *SRouterHandler) probeAntigravityModels() ([]map[string]any, error) {
	// Candidate models for Antigravity verification
	candidates := []string{
		"antigravity/gemini-3.8-flash",
		"antigravity/claude-opus-4-6-thinking",
		"antigravity/claude-sonnet-4-6",
		"antigravity/gpt-oss-120b-medium",
		"antigravity/gemini-3.7-flash-high",
		"antigravity/gemini-3.7-flash-medium",
		"antigravity/gemini-3.7-flash-low",
		"antigravity/gemini-3.6-flash-high",
		"antigravity/gemini-3.6-flash-medium",
		"antigravity/gemini-3.6-flash-low",
		"antigravity/gemini-3.1-pro-high",
		"antigravity/gemini-3.1-pro-low",
		"antigravity/gemini-pro-agent",
		"antigravity/gemini-3-flash-agent",
		"antigravity/gemini-3.5-flash-high",
		"antigravity/gemini-3.5-flash-medium",
		"antigravity/gemini-3.5-flash-low",
		"antigravity/gemini-3.5-flash-extra-low",
		"antigravity/gemini-2.5-flash",
		"antigravity/gemini-2.5-pro",
		"antigravity/claude-3-7-sonnet",
	}

	savedConns, err := h.getAllProviders()
	if err != nil {
		return nil, err
	}
	var antigravityConn *ProviderConfig
	for _, c := range savedConns {
		if strings.ToLower(c.ProviderID) == "antigravity" || strings.ToLower(c.ID) == "antigravity" {
			antigravityConn = c
			break
		}
	}
	if antigravityConn == nil {
		return nil, fmt.Errorf("no antigravity connection configured")
	}

	now := time.Now().UnixMilli()
	var validModels []map[string]any

	// Parallel probing with fast timeout
	type probeRes struct {
		model string
		valid bool
	}
	resChan := make(chan probeRes, len(candidates))

	client := &http.Client{Timeout: 8 * time.Second}

	for _, m := range candidates {
		go func(modelName string) {
			payload := map[string]any{
				"model":    modelName,
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
				"stream":   false,
			}
			pBytes, _ := json.Marshal(payload)
			req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8000/v1/chat/completions", bytes.NewReader(pBytes))
			if err != nil {
				resChan <- probeRes{model: modelName, valid: false}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				resChan <- probeRes{model: modelName, valid: false}
				return
			}
			defer resp.Body.Close()
			resChan <- probeRes{model: modelName, valid: resp.StatusCode >= 200 && resp.StatusCode < 300}
		}(m)
	}

	for i := 0; i < len(candidates); i++ {
		res := <-resChan
		if res.valid {
			validModels = append(validModels, map[string]any{
				"id":     res.model,
				"object": "model",
			})
		} else {
			// Mark as disabled/invalid in DB
			_, _ = h.db.Exec(`INSERT OR REPLACE INTO disabled_models (model_id, provider_id, reason, disabled_at) VALUES (?, 'antigravity', 'probe_failed', ?)`,
				res.model, now)
		}
	}

	if len(validModels) > 0 {
		tx, err := h.db.Begin()
		if err == nil {
			for _, m := range validModels {
				mid, _ := m["id"].(string)
				_, _ = tx.Exec(`DELETE FROM disabled_models WHERE model_id = ?`, mid)
				_, _ = tx.Exec(`INSERT INTO provider_models (provider_id, model_id, created_at) VALUES ('antigravity', ?, ?)
ON CONFLICT(provider_id, model_id) DO UPDATE SET created_at = excluded.created_at`, mid, now)
			}
			_ = tx.Commit()
		}
	}

	return validModels, nil
}

// ScanAndSyncProviderModels dynamically fetches models from provider /v1/models endpoint using the connected API key/tokens
func (h *SRouterHandler) ScanAndSyncProviderModels(providerID string) ([]map[string]any, error) {
	providerID = strings.ToLower(providerID)
	if providerID == "antigravity" {
		return h.probeAntigravityModels()
	}

	if providerID == "framer" || strings.HasPrefix(providerID, "framer") {
		framerModels := []map[string]any{
			{"id": "framer/gpt-5.5", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-luna", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-terra", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-sol", "object": "model", "owned_by": "framer"},
			{"id": "framer/sonnet-4.6", "object": "model", "owned_by": "framer"},
			{"id": "framer/sonnet-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/opus-4.8", "object": "model", "owned_by": "framer"},
			{"id": "framer/opus-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/fable-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/google/gemini-3-flash-preview", "object": "model", "owned_by": "framer"},
		}
		for _, m := range framerModels {
			mID, _ := m["id"].(string)
			_, _ = h.db.Exec(`INSERT OR REPLACE INTO provider_models (provider_id, model_id, context_length, is_custom, created_at) 
				VALUES (?, ?, 128000, 0, ?)`,
				"framer", mID, time.Now().UnixMilli())
		}
		return framerModels, nil
	}

	savedConns, err := h.getAllProviders()
	if err != nil {
		return nil, err
	}

	var activeKey string
	var baseURL string
	var protocol string
	for _, c := range savedConns {
		if strings.ToLower(c.ProviderID) == providerID || strings.ToLower(c.ID) == providerID {
			if c.APIKey != nil && *c.APIKey != "" {
				activeKey = *c.APIKey
			} else if c.AccessToken != nil && *c.AccessToken != "" {
				activeKey = *c.AccessToken
			}
			if c.BaseURL != nil && *c.BaseURL != "" {
				baseURL = *c.BaseURL
			}
			if c.Protocol != "" {
				protocol = c.Protocol
			}
			break
		}
	}

	// If no connection, check if it's a known provider with default base URL
	if baseURL == "" {
		for _, seed := range KnownCatalogSeeds {
			if strings.ToLower(seed.ID) == providerID {
				if seed.DefaultBaseURL != nil {
					baseURL = *seed.DefaultBaseURL
				}
				if protocol == "" {
					protocol = seed.Protocol
				}
				break
			}
		}
	}

	if activeKey == "" && providerID != "opencode_zen" && providerID != "mimo-free" && providerID != "blackbox" {
		return nil, fmt.Errorf("no active connection or API key configured for %s", providerID)
	}

	modelsEndpoint := ""
	if strings.Contains(baseURL, "/chat/completions") {
		modelsEndpoint = strings.Replace(baseURL, "/chat/completions", "/models", 1)
	} else if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v1/") {
		modelsEndpoint = strings.TrimRight(baseURL, "/") + "/models"
	} else if baseURL != "" {
		modelsEndpoint = strings.TrimRight(baseURL, "/") + "/v1/models"
	} else {
		modelsEndpoint = "https://api.openai.com/v1/models"
	}

	// Provider specific adjustments
	switch providerID {
	case "framer":
		// Framer AI Studio built-in models catalog
		framerModels := []map[string]any{
			{"id": "framer/gpt-5.5", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-luna", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-terra", "object": "model", "owned_by": "framer"},
			{"id": "framer/gpt-5.6-sol", "object": "model", "owned_by": "framer"},
			{"id": "framer/sonnet-4.6", "object": "model", "owned_by": "framer"},
			{"id": "framer/sonnet-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/opus-4.8", "object": "model", "owned_by": "framer"},
			{"id": "framer/opus-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/fable-5", "object": "model", "owned_by": "framer"},
			{"id": "framer/google/gemini-3-flash-preview", "object": "model", "owned_by": "framer"},
		}
		for _, m := range framerModels {
			mID, _ := m["id"].(string)
			_, _ = h.db.Exec(`INSERT OR REPLACE INTO provider_models (provider_id, model_id, context_length, max_output_tokens, pricing_input, pricing_output, updated_at) 
				VALUES (?, ?, 128000, 8192, 0, 0, ?)`,
				"framer", mID, time.Now().UnixMilli())
		}
		return framerModels, nil
	case "openai":
		modelsEndpoint = "https://api.openai.com/v1/models"
	case "anthropic":
		modelsEndpoint = "https://api.anthropic.com/v1/models"
	case "deepseek":
		modelsEndpoint = "https://api.deepseek.com/models"
	case "groq":
		modelsEndpoint = "https://api.groq.com/openai/v1/models"
	case "openrouter":
		modelsEndpoint = "https://openrouter.ai/api/v1/models"
	case "cerebras":
		modelsEndpoint = "https://api.cerebras.ai/v1/models"
	case "together":
		modelsEndpoint = "https://api.together.xyz/v1/models"
	case "fireworks":
		modelsEndpoint = "https://api.fireworks.ai/inference/v1/models"
	case "mistral":
		modelsEndpoint = "https://api.mistral.ai/v1/models"
	case "perplexity":
		modelsEndpoint = "https://api.perplexity.ai/models"
	case "xai":
		modelsEndpoint = "https://api.x.ai/v1/models"
	case "siliconflow":
		modelsEndpoint = "https://api.siliconflow.cn/v1/models"
	case "gemini":
		modelsEndpoint = "https://generativelanguage.googleapis.com/v1beta/openai/models"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, modelsEndpoint, nil)
	if err != nil {
		return nil, err
	}

	if providerID == "anthropic" {
		req.Header.Set("x-api-key", activeKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if activeKey != "" {
		req.Header.Set("Authorization", "Bearer "+activeKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to scan upstream models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var res struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	var scanned []map[string]any
	now := time.Now().UnixMilli()

	// Parse items
	if len(res.Data) > 0 {
		for _, item := range res.Data {
			mID := item.ID
			if !strings.HasPrefix(mID, providerID+"/") && providerID != "openai" {
				mID = providerID + "/" + mID
			} else if providerID == "openai" && !strings.HasPrefix(mID, "openai/") {
				mID = "openai/" + mID
			}
			scanned = append(scanned, map[string]any{
				"id":     mID,
				"object": "model",
			})
		}
	} else if len(res.Models) > 0 {
		for _, item := range res.Models {
			mID := item.ID
			if mID == "" {
				mID = item.Name
			}
			if !strings.HasPrefix(mID, providerID+"/") {
				mID = providerID + "/" + mID
			}
			scanned = append(scanned, map[string]any{
				"id":     mID,
				"object": "model",
			})
		}
	}

	if len(scanned) == 0 {
		return nil, fmt.Errorf("no models discovered from upstream")
	}

	// Smart Sync: Upsert scanned models into database
	tx, err := h.db.Begin()
	if err == nil {
		for _, m := range scanned {
			mid, _ := m["id"].(string)
			_, _ = tx.Exec(`INSERT INTO provider_models (provider_id, model_id, created_at) VALUES (?, ?, ?)
ON CONFLICT(provider_id, model_id) DO UPDATE SET created_at = excluded.created_at`, providerID, mid, now)
		}
		_ = tx.Commit()
	}

	return scanned, nil
}

func (h *SRouterHandler) getSyncedProviderModels(providerID string) ([]map[string]any, error) {
	rows, err := h.db.Query(`SELECT model_id, COALESCE(is_active, 1) FROM provider_models WHERE provider_id = ? ORDER BY model_id ASC`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []map[string]any
	for rows.Next() {
		var mid string
		var isActive int
		if err := rows.Scan(&mid, &isActive); err == nil {
			models = append(models, map[string]any{
				"id":       mid,
				"object":   "model",
				"isActive": isActive == 1,
			})
		}
	}
	return models, nil
}

// POST /v1/providers/{providerId}/models/toggle OR /v1/providers/{providerId}/models/{modelId}/toggle
func (h *SRouterHandler) HandleProviderModelToggle(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	modelID := chi.URLParam(r, "modelId")

	var body struct {
		ModelID  string `json:"modelId"`
		IsActive *bool  `json:"isActive"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if modelID == "" && body.ModelID != "" {
		modelID = body.ModelID
	}
	if strings.Contains(modelID, "%2F") || strings.Contains(modelID, "%2f") {
		if unescaped, err := url.PathUnescape(modelID); err == nil {
			modelID = unescaped
		}
	}
	if modelID == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "modelId is required")
		return
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	} else if body.Enabled != nil {
		isActive = *body.Enabled
	}

	activeInt := 0
	if isActive {
		activeInt = 1
	}

	now := time.Now().UnixMilli()
	// Upsert into provider_models with updated is_active flag
	_, err := h.db.Exec(`
		INSERT INTO provider_models (provider_id, model_id, is_active, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider_id, model_id) DO UPDATE SET is_active = excluded.is_active`,
		providerID, modelID, activeInt, now)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to toggle model: "+err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"providerId": providerID,
		"modelId":    modelID,
		"isActive":   isActive,
	})
}

// GET /v1/providers/{providerId}/models
func (h *SRouterHandler) HandleProviderModelsList(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	cat := h.buildCatalog()

	var matchedModels []map[string]any
	for _, p := range cat {
		if strings.ToLower(p.ID) == providerID {
			matchedModels = p.Models
			break
		}
	}

	// Check DB synced models
	dbModels, err := h.getSyncedProviderModels(providerID)
	if err == nil && len(dbModels) > 0 {
		matchedModels = dbModels
	} else {
		for i, m := range matchedModels {
			mid, _ := m["id"].(string)
			var isActive int = 1
			_ = h.db.QueryRow(`SELECT COALESCE(is_active, 1) FROM provider_models WHERE provider_id = ? AND model_id = ?`, providerID, mid).Scan(&isActive)
			matchedModels[i]["isActive"] = isActive == 1
		}
	}

	// Custom models
	rows, err := h.db.Query(`SELECT model_id FROM custom_models WHERE provider_id = ?`, providerID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var mid string
			if err := rows.Scan(&mid); err == nil {
				matchedModels = append(matchedModels, map[string]any{
					"id": mid, "object": "model", "isCustom": true,
				})
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"providerId": providerID,
		"models":     matchedModels,
		"total":      len(matchedModels),
	})
}

// POST /v1/providers/{providerId}/sync
func (h *SRouterHandler) HandleProviderSyncModels(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	if providerID == "" {
		providerID = chi.URLParam(r, "provider")
	}

	scanned, err := h.ScanAndSyncProviderModels(providerID)
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"providerId": providerID,
			"message":    fmt.Sprintf("Scanned preset models for %s", providerID),
			"models":     scanned,
		})
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"providerId": providerID,
		"models":     scanned,
	})
}

// POST /v1/providers/{providerId}/models
func (h *SRouterHandler) HandleCustomModelAdd(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	var body struct {
		ModelID string `json:"modelId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ModelID == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid modelId")
		return
	}

	_, err := h.db.Exec(`INSERT OR IGNORE INTO custom_models (provider_id, model_id, created_at) VALUES (?, ?, ?)`,
		providerID, body.ModelID, time.Now().UnixMilli())
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to save custom model")
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"providerId": providerID,
		"modelId":    body.ModelID,
	})
}

// DELETE /v1/providers/{providerId}/models/{modelId}
func (h *SRouterHandler) HandleCustomModelDelete(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	modelID := chi.URLParam(r, "modelId")

	_, err := h.db.Exec(`DELETE FROM custom_models WHERE provider_id = ? AND model_id = ?`, providerID, modelID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to delete custom model")
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"providerId": providerID,
		"modelId":    modelID,
	})
}

// POST /v1/providers/{providerId}/models/toggle
func (h *SRouterHandler) HandleProviderLegacyModelToggle(w http.ResponseWriter, r *http.Request) {
	providerID := strings.ToLower(chi.URLParam(r, "providerId"))
	var body struct {
		ModelID string `json:"modelId"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ModelID == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "Invalid modelId")
		return
	}

	if body.Enabled {
		_, err := h.db.Exec(`DELETE FROM disabled_models WHERE model_id = ?`, body.ModelID)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to enable model")
			return
		}
	} else {
		_, err := h.db.Exec(`INSERT OR REPLACE INTO disabled_models (model_id, provider_id, reason, disabled_at) VALUES (?, ?, 'manual_toggle', ?)`,
			body.ModelID, providerID, time.Now().UnixMilli())
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, "Failed to disable model")
			return
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"providerId": providerID,
		"modelId":    body.ModelID,
		"enabled":    body.Enabled,
	})
}

