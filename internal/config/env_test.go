package config

import "testing"

func TestApplySecretsFromEnv(t *testing.T) {
	t.Setenv("MEOWCLAW_OPENAI_API_KEY", "env-openai")
	t.Setenv("MEOWCLAW_ANTHROPIC_API_KEY", "env-anthropic")
	t.Setenv("MEOWCLAW_GATEWAY_API_TOKEN", "env-gw-token")

	cfg := DefaultConfig()
	cfg.Agent.OpenAI = &OpenAIConfig{APIKey: "file-openai"}
	cfg.Agent.Anthropic = &AnthropicConfig{APIKey: "file-anthropic"}
	cfg.Gateway.APIToken = "file-gw"

	ApplySecretsFromEnv(cfg)

	if cfg.Agent.OpenAI.APIKey != "env-openai" {
		t.Errorf("openai key = %q, want env override", cfg.Agent.OpenAI.APIKey)
	}
	if cfg.Agent.Anthropic.APIKey != "env-anthropic" {
		t.Errorf("anthropic key = %q, want env override", cfg.Agent.Anthropic.APIKey)
	}
	if cfg.Gateway.APIToken != "env-gw-token" {
		t.Errorf("gateway token = %q, want env override", cfg.Gateway.APIToken)
	}
}

func TestApplySecretsFromEnvNil(t *testing.T) {
	ApplySecretsFromEnv(nil) // must not panic
}
