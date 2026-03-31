package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Gateway  GatewayConfig  `yaml:"gateway"`
	Channels ChannelsConfig `yaml:"channels"`
	Agent    AgentConfig    `yaml:"agent"`
	Memory   MemoryConfig   `yaml:"memory"`
}

type GatewayConfig struct {
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
	Verbose bool   `yaml:"verbose"`
}

type ChannelsConfig struct {
	Telegram *TelegramConfig `yaml:"telegram,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty"`
	WhatsApp *WhatsAppConfig `yaml:"whatsapp,omitempty"`
	WebChat  *WebChatConfig  `yaml:"webchat,omitempty"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
}

type DiscordConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
}

type WhatsAppConfig struct {
	Enabled  bool   `yaml:"enabled"`
	DBPath   string `yaml:"db_path"`
}

type WebChatConfig struct {
	Enabled bool `yaml:"enabled"`
}

type AgentConfig struct {
	Model    string          `yaml:"model"`
	Provider string          `yaml:"provider"`
	OpenAI   *OpenAIConfig   `yaml:"openai,omitempty"`
	Anthropic *AnthropicConfig `yaml:"anthropic,omitempty"`
}

type OpenAIConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"`
}

type AnthropicConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"`
}

type MemoryConfig struct {
	Enabled bool   `yaml:"enabled"`
	DBPath  string `yaml:"db_path"`
}

func DefaultConfig() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Port: 6820,
			Host: "127.0.0.1",
		},
		Channels: ChannelsConfig{
			WebChat: &WebChatConfig{Enabled: true},
		},
		Agent: AgentConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		Memory: MemoryConfig{
			Enabled: true,
			DBPath:  defaultDBPath(),
		},
	}
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meowclaw", "memory.db")
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meowclaw", "config.yaml")
}
