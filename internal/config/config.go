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

	// APIToken, if set, requires Bearer token or (for WebSocket) ?token= for /api/send, /api/channels, /ws.
	APIToken string `yaml:"api_token,omitempty"`

	// TrustedOrigins lists allowed WebSocket Origin values. Empty defaults to this host + localhost on gateway.port.
	TrustedOrigins []string `yaml:"trusted_origins,omitempty"`

	// AllowAnyWebsocketOrigin disables Origin checks (not recommended; exposes CSRF-style abuse if gateway is reachable).
	AllowAnyWebsocketOrigin bool `yaml:"allow_any_websocket_origin,omitempty"`

	// MaxAPIBodyBytes caps JSON body size for /api/send (0 = DefaultMaxAPIBodyBytes).
	MaxAPIBodyBytes int64 `yaml:"max_api_body_bytes,omitempty"`
}

type ChannelsConfig struct {
	Telegram *TelegramConfig `yaml:"telegram,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty"`
	WhatsApp *WhatsAppConfig `yaml:"whatsapp,omitempty"`
	Slack    *SlackConfig    `yaml:"slack,omitempty"`
	WebChat  *WebChatConfig  `yaml:"webchat,omitempty"`
	LINE     *LINEConfig     `yaml:"line,omitempty"`
	Kakao    *KakaoConfig    `yaml:"kakao,omitempty"`
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
	Enabled bool   `yaml:"enabled"`
	DBPath  string `yaml:"db_path"`
}

type SlackConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	AppToken string `yaml:"app_token"`
}

type WebChatConfig struct {
	Enabled bool `yaml:"enabled"`
}

type LINEConfig struct {
	Enabled       bool   `yaml:"enabled"`
	ChannelSecret string `yaml:"channel_secret"`
	AccessToken   string `yaml:"access_token"`
}

type KakaoConfig struct {
	Enabled bool `yaml:"enabled"`
}

type AgentConfig struct {
	Model         string           `yaml:"model"`
	Provider      string           `yaml:"provider"`
	OpenAI        *OpenAIConfig    `yaml:"openai,omitempty"`
	Anthropic     *AnthropicConfig `yaml:"anthropic,omitempty"`
	Gemini        *GeminiConfig    `yaml:"gemini,omitempty"`
	MaxInputRunes int              `yaml:"max_input_runes,omitempty"` // 0 = DefaultMaxInputRunes
}

type OpenAIConfig struct {
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url,omitempty"`  // host only, e.g. https://api.openai.com
	ChatPath string `yaml:"chat_path,omitempty"` // e.g. /v1/chat/completions — empty uses provider default
}

type AnthropicConfig struct {
	APIKey       string `yaml:"api_key"`
	Model        string `yaml:"model"`
	BaseURL      string `yaml:"base_url,omitempty"`      // host only
	MessagesPath string `yaml:"messages_path,omitempty"` // e.g. /v1/messages — empty uses provider default
}

type GeminiConfig struct {
	APIKey       string `yaml:"api_key"`
	Model        string `yaml:"model"`
	BaseURL      string `yaml:"base_url,omitempty"`      // host only, e.g. https://generativelanguage.googleapis.com
	GeneratePath string `yaml:"generate_path,omitempty"` // e.g. /v1beta/models/%s:generateContent — empty uses default
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meowclaw", "config.yaml")
}
