package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/YoungsoonLee/meowclaw/internal/agent"
	"github.com/YoungsoonLee/meowclaw/internal/agent/provider"
	"github.com/YoungsoonLee/meowclaw/internal/channel/discord"
	"github.com/YoungsoonLee/meowclaw/internal/channel/kakao"
	"github.com/YoungsoonLee/meowclaw/internal/channel/line"
	slackchan "github.com/YoungsoonLee/meowclaw/internal/channel/slack"
	"github.com/YoungsoonLee/meowclaw/internal/channel/telegram"
	"github.com/YoungsoonLee/meowclaw/internal/channel/webchat"
	"github.com/YoungsoonLee/meowclaw/internal/channel/whatsapp"
	"github.com/YoungsoonLee/meowclaw/internal/config"
	"github.com/YoungsoonLee/meowclaw/internal/gateway"
	"github.com/YoungsoonLee/meowclaw/internal/memory"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

var version = "dev"

// agentRuntimeMeta derives provider label and default model for /status and /model help.
func agentRuntimeMeta(cfg *config.Config) (providerName, model string) {
	providerName = cfg.Agent.Provider
	if providerName == "" {
		providerName = "openai"
	}
	model = cfg.Agent.Model
	switch cfg.Agent.Provider {
	case "anthropic":
		if cfg.Agent.Anthropic != nil && strings.TrimSpace(cfg.Agent.Anthropic.Model) != "" {
			model = cfg.Agent.Anthropic.Model
		}
	case "gemini":
		if cfg.Agent.Gemini != nil && strings.TrimSpace(cfg.Agent.Gemini.Model) != "" {
			model = cfg.Agent.Gemini.Model
		}
	default:
		if cfg.Agent.OpenAI != nil && strings.TrimSpace(cfg.Agent.OpenAI.Model) != "" {
			model = cfg.Agent.OpenAI.Model
		}
	}
	return providerName, model
}

func main() {
	root := &cobra.Command{
		Use:     "meowclaw",
		Short:   "MeowClaw - Lightweight AI Gateway",
		Long:    "A fast, stable, single-binary AI assistant gateway.\nBuilt with Go. Designed to never crash. 🐱",
		Version: version,
	}

	root.AddCommand(initCmd(), upCmd(), statusCmd(), sendCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)
			cfg := config.DefaultConfig()

			fmt.Println("🐱 MeowClaw Setup")
			fmt.Println("─────────────────────────────────")
			fmt.Println()

			// AI provider
			fmt.Print("AI Provider [openai/anthropic/gemini] (openai): ")
			prov, _ := reader.ReadString('\n')
			prov = strings.TrimSpace(prov)
			if prov == "" {
				prov = "openai"
			}
			cfg.Agent.Provider = prov

			fmt.Print("API Key: ")
			apiKey, _ := reader.ReadString('\n')
			apiKey = strings.TrimSpace(apiKey)

			switch prov {
			case "openai":
				cfg.Agent.OpenAI = &config.OpenAIConfig{APIKey: apiKey, Model: "gpt-4o"}
			case "anthropic":
				cfg.Agent.Anthropic = &config.AnthropicConfig{APIKey: apiKey, Model: "claude-sonnet-4-20250514"}
			case "gemini":
				cfg.Agent.Gemini = &config.GeminiConfig{APIKey: apiKey, Model: "gemini-2.5-flash"}
			}

			// Channels
			fmt.Println()
			fmt.Println("Channels (press Enter to skip)")

			fmt.Print("Telegram Bot Token: ")
			tgToken, _ := reader.ReadString('\n')
			tgToken = strings.TrimSpace(tgToken)
			if tgToken != "" {
				cfg.Channels.Telegram = &config.TelegramConfig{Enabled: true, BotToken: tgToken}
			}

			fmt.Print("Discord Bot Token: ")
			dcToken, _ := reader.ReadString('\n')
			dcToken = strings.TrimSpace(dcToken)
			if dcToken != "" {
				cfg.Channels.Discord = &config.DiscordConfig{Enabled: true, BotToken: dcToken}
			}

			fmt.Print("Slack Bot Token (xoxb-...): ")
			slackBot, _ := reader.ReadString('\n')
			slackBot = strings.TrimSpace(slackBot)
			if slackBot != "" {
				fmt.Print("Slack App Token (xapp-...): ")
				slackApp, _ := reader.ReadString('\n')
				slackApp = strings.TrimSpace(slackApp)
				cfg.Channels.Slack = &config.SlackConfig{Enabled: true, BotToken: slackBot, AppToken: slackApp}
			}

			fmt.Print("Enable WhatsApp? [y/N]: ")
			waAnswer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(waAnswer)) == "y" {
				cfg.Channels.WhatsApp = &config.WhatsAppConfig{Enabled: true}
			}

			fmt.Print("LINE Channel Secret: ")
			lineSecret, _ := reader.ReadString('\n')
			lineSecret = strings.TrimSpace(lineSecret)
			if lineSecret != "" {
				fmt.Print("LINE Channel Access Token: ")
				lineToken, _ := reader.ReadString('\n')
				lineToken = strings.TrimSpace(lineToken)
				cfg.Channels.LINE = &config.LINEConfig{Enabled: true, ChannelSecret: lineSecret, AccessToken: lineToken}
			}

			fmt.Print("Enable Kakao? [y/N]: ")
			kakaoAnswer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(kakaoAnswer)) == "y" {
				cfg.Channels.Kakao = &config.KakaoConfig{Enabled: true}
			}

			// Gateway
			fmt.Printf("\nGateway port (%d): ", cfg.Gateway.Port)
			portStr, _ := reader.ReadString('\n')
			portStr = strings.TrimSpace(portStr)
			if portStr != "" {
				fmt.Sscanf(portStr, "%d", &cfg.Gateway.Port)
			}

			fmt.Print("Gateway API token (optional, protects /api/send and /ws; Enter to skip): ")
			apiTok, _ := reader.ReadString('\n')
			apiTok = strings.TrimSpace(apiTok)
			if apiTok != "" {
				cfg.Gateway.APIToken = apiTok
			}

			cfgPath := config.DefaultConfigPath()
			if err := cfg.Save(cfgPath); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Println()
			fmt.Printf("Config saved to %s\n", cfgPath)
			fmt.Println("Run `meowclaw up` to start the gateway.")
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	var cfgPath string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}

			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			config.ApplySecretsFromEnv(cfg)

			if verbose {
				cfg.Gateway.Verbose = true
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Trap signals
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

			gw := gateway.New(cfg)

			// Memory store
			var mem *memory.Store
			if cfg.Memory.Enabled {
				mem, err = memory.New(cfg.Memory.DBPath)
				if err != nil {
					slog.Warn("memory store unavailable, running without persistence", "error", err)
				}
			}

			// LLM provider
			var llmProvider provider.Provider
			switch cfg.Agent.Provider {
			case "anthropic":
				if cfg.Agent.Anthropic != nil {
					llmProvider = provider.NewAnthropic(
						cfg.Agent.Anthropic.APIKey,
						cfg.Agent.Anthropic.Model,
						cfg.Agent.Anthropic.BaseURL,
						cfg.Agent.Anthropic.MessagesPath,
					)
				}
			case "gemini":
				if cfg.Agent.Gemini != nil {
					llmProvider = provider.NewGemini(
						cfg.Agent.Gemini.APIKey,
						cfg.Agent.Gemini.Model,
						cfg.Agent.Gemini.BaseURL,
						cfg.Agent.Gemini.GeneratePath,
					)
				}
			default:
				if cfg.Agent.OpenAI != nil {
					llmProvider = provider.NewOpenAI(
						cfg.Agent.OpenAI.APIKey,
						cfg.Agent.OpenAI.Model,
						cfg.Agent.OpenAI.BaseURL,
						cfg.Agent.OpenAI.ChatPath,
					)
				}
			}

			// Agent
			var ai *agent.Agent
			if llmProvider != nil {
				opts := []agent.Option{
					agent.WithRuntimeMeta(agentRuntimeMeta(cfg)),
				}
				if mem != nil {
					opts = append(opts, agent.WithMemory(mem))
				}
				if cfg.Agent.MaxInputRunes > 0 {
					opts = append(opts, agent.WithMaxInputRunes(cfg.Agent.MaxInputRunes))
				}
				ai = agent.New(llmProvider, opts...)
			}

			// Register channels
			if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled {
				gw.RegisterChannel(telegram.New(cfg.Channels.Telegram.BotToken))
			}
			if cfg.Channels.Discord != nil && cfg.Channels.Discord.Enabled {
				gw.RegisterChannel(discord.New(cfg.Channels.Discord.BotToken))
			}
			if cfg.Channels.WhatsApp != nil && cfg.Channels.WhatsApp.Enabled {
				dbPath := cfg.Channels.WhatsApp.DBPath
				gw.RegisterChannel(whatsapp.New(dbPath))
			}
			if cfg.Channels.Slack != nil && cfg.Channels.Slack.Enabled {
				gw.RegisterChannel(slackchan.New(cfg.Channels.Slack.BotToken, cfg.Channels.Slack.AppToken))
			}
			if cfg.Channels.LINE != nil && cfg.Channels.LINE.Enabled {
				gw.RegisterChannel(line.New(cfg.Channels.LINE.ChannelSecret, cfg.Channels.LINE.AccessToken))
			}
			if cfg.Channels.Kakao != nil && cfg.Channels.Kakao.Enabled {
				gw.RegisterChannel(kakao.New())
			}

			wc := webchat.New()
			if cfg.Channels.WebChat == nil || cfg.Channels.WebChat.Enabled {
				gw.RegisterChannel(wc)
			}

			// Wire agent to handle inbound messages (streaming when supported)
			if ai != nil {
				if ai.SupportsStreaming() {
					gw.OnStream(func(ctx context.Context, msg *message.Message, onChunk func(string)) *message.Message {
						reply, err := ai.ProcessStream(ctx, msg, onChunk)
						if err != nil {
							slog.Error("agent error", "session", msg.SessionID, "error", err)
							return nil
						}
						return reply
					})
				} else {
					gw.OnMessage(func(ctx context.Context, msg *message.Message) *message.Message {
						reply, err := ai.Process(ctx, msg)
						if err != nil {
							slog.Error("agent error", "session", msg.SessionID, "error", err)
							return nil
						}
						return reply
					})
				}
			}

			fmt.Println("🐱 MeowClaw gateway starting...")
			fmt.Printf("   Dashboard: http://%s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
			fmt.Printf("   WebSocket: ws://%s:%d/ws\n", cfg.Gateway.Host, cfg.Gateway.Port)
			if strings.TrimSpace(cfg.Gateway.APIToken) != "" {
				fmt.Println("   WebChat: append ?token=<gateway.api_token> to the dashboard URL (token is not logged).")
			}
			fmt.Println()

			// Start gateway in goroutine
			errCh := make(chan error, 1)
			go func() {
				errCh <- gw.Start(ctx)
			}()

			select {
			case <-sig:
				fmt.Println("\nShutting down...")
				shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer shutCancel()
				gw.Stop(shutCtx)
				if mem != nil {
					mem.Close()
				}
				cancel()
			case err := <-errCh:
				if err != nil {
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	return cmd
}

func statusCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check gateway status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
			if err != nil {
				fmt.Println("Gateway is not running.")
				return nil
			}
			defer resp.Body.Close()

			buf := make([]byte, 4096)
			n, _ := resp.Body.Read(buf)
			fmt.Println(string(buf[:n]))
			return nil
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 6820, "gateway port")
	return cmd
}

func sendCmd() *cobra.Command {
	var channel, to, text string
	var port int
	var apiToken string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message through a channel",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := json.Marshal(map[string]string{
				"channel": channel,
				"to":      to,
				"text":    text,
			})
			if err != nil {
				return err
			}

			req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/send", port), bytes.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			tok := strings.TrimSpace(apiToken)
			if tok == "" {
				tok = strings.TrimSpace(os.Getenv("MEOWCLAW_GATEWAY_API_TOKEN"))
			}
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("send failed: %w", err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("unauthorized (set gateway.api_token in config, --api-token, or MEOWCLAW_GATEWAY_API_TOKEN)")
			}
			fmt.Println(string(respBody))
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "target channel (telegram/discord/whatsapp)")
	cmd.Flags().StringVar(&to, "to", "", "recipient (chat ID, channel ID, or JID)")
	cmd.Flags().StringVarP(&text, "message", "m", "", "message text")
	cmd.Flags().IntVarP(&port, "port", "p", 6820, "gateway port")
	cmd.Flags().StringVar(&apiToken, "api-token", "", "gateway API token (or env MEOWCLAW_GATEWAY_API_TOKEN)")
	cmd.MarkFlagRequired("channel")
	cmd.MarkFlagRequired("to")
	cmd.MarkFlagRequired("message")
	return cmd
}
