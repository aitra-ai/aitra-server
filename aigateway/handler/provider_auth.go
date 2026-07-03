package handler

import (
	"encoding/json"
	"strings"

	"github.com/aitra-ai/aitra-server/common/config"
)

// buildProviderAuthHead constructs the auth header JSON for an external model call.
// Priority: config-level provider API key > DB-stored auth_header.
// Returns an empty string if no key is available.
func buildProviderAuthHead(cfg *config.Config, provider, endpoint, dbAuthHead string) string {
	provider = strings.ToLower(provider)

	// Determine provider from endpoint URL if not explicitly set
	if provider == "" {
		switch {
		case strings.Contains(endpoint, "anthropic.com"):
			provider = "anthropic"
		case strings.Contains(endpoint, "openai.com"):
			provider = "openai"
		case strings.Contains(endpoint, "openrouter.ai"):
			provider = "openrouter"
		case strings.Contains(endpoint, "deepseek.com"):
			provider = "deepseek"
		}
	}

	// Build auth header from config API keys (takes precedence over DB)
	switch provider {
	case "anthropic":
		if cfg.AIGateway.AnthropicAPIKey != "" {
			m := map[string]string{
				"x-api-key":           cfg.AIGateway.AnthropicAPIKey,
				"anthropic-version":   "2023-06-01",
			}
			b, _ := json.Marshal(m)
			return string(b)
		}
	case "openai":
		if cfg.AIGateway.OpenAIAPIKey != "" {
			m := map[string]string{"Authorization": "Bearer " + cfg.AIGateway.OpenAIAPIKey}
			b, _ := json.Marshal(m)
			return string(b)
		}
	case "openrouter":
		if cfg.AIGateway.OpenRouterAPIKey != "" {
			m := map[string]string{"Authorization": "Bearer " + cfg.AIGateway.OpenRouterAPIKey}
			b, _ := json.Marshal(m)
			return string(b)
		}
	case "deepseek":
		if cfg.AIGateway.DeepSeekAPIKey != "" {
			m := map[string]string{"Authorization": "Bearer " + cfg.AIGateway.DeepSeekAPIKey}
			b, _ := json.Marshal(m)
			return string(b)
		}
	}

	// Fall back to DB-stored auth_header
	return dbAuthHead
}
