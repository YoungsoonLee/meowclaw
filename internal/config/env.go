package config

import "os"

// ApplySecretsFromEnv overlays sensitive values from the environment (after Load).
// Env wins over config file — use for production so keys are not only on disk.
//
//	MEOWCLAW_OPENAI_API_KEY
//	MEOWCLAW_ANTHROPIC_API_KEY
//	MEOWCLAW_GATEWAY_API_TOKEN
func ApplySecretsFromEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("MEOWCLAW_OPENAI_API_KEY"); v != "" && cfg.Agent.OpenAI != nil {
		cfg.Agent.OpenAI.APIKey = v
	}
	if v := os.Getenv("MEOWCLAW_ANTHROPIC_API_KEY"); v != "" && cfg.Agent.Anthropic != nil {
		cfg.Agent.Anthropic.APIKey = v
	}
	if v := os.Getenv("MEOWCLAW_GATEWAY_API_TOKEN"); v != "" {
		cfg.Gateway.APIToken = v
	}
}
