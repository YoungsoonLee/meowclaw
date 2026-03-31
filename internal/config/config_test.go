package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Gateway.Port != 6820 {
		t.Errorf("default port = %d, want 6820", cfg.Gateway.Port)
	}
	if cfg.Gateway.Host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", cfg.Gateway.Host)
	}
	if cfg.Agent.Provider != "openai" {
		t.Errorf("default provider = %q, want openai", cfg.Agent.Provider)
	}
	if cfg.Agent.Model != "gpt-4o" {
		t.Errorf("default model = %q, want gpt-4o", cfg.Agent.Model)
	}
	if !cfg.Memory.Enabled {
		t.Error("memory should be enabled by default")
	}
	if cfg.Channels.WebChat == nil || !cfg.Channels.WebChat.Enabled {
		t.Error("webchat should be enabled by default")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Gateway.Port = 9999
	cfg.Agent.Provider = "anthropic"
	cfg.Channels.Telegram = &TelegramConfig{
		Enabled:  true,
		BotToken: "test-token-123",
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Gateway.Port != 9999 {
		t.Errorf("loaded port = %d, want 9999", loaded.Gateway.Port)
	}
	if loaded.Agent.Provider != "anthropic" {
		t.Errorf("loaded provider = %q, want anthropic", loaded.Agent.Provider)
	}
	if loaded.Channels.Telegram == nil {
		t.Fatal("loaded telegram config is nil")
	}
	if loaded.Channels.Telegram.BotToken != "test-token-123" {
		t.Errorf("loaded bot token = %q, want test-token-123", loaded.Channels.Telegram.BotToken)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load("/tmp/meowclaw-test-nonexistent-config.yaml")
	if err != nil {
		t.Fatalf("loading non-existent file should not error: %v", err)
	}
	if cfg.Gateway.Port != 6820 {
		t.Errorf("should return defaults, got port = %d", cfg.Gateway.Port)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("{{invalid yaml:::"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "config.yaml")

	cfg := DefaultConfig()
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save to nested dir: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}
