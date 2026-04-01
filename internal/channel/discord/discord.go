package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

type Channel struct {
	token     string
	session   *discordgo.Session
	incoming  chan *message.Message
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	lastErr   string
	botID     string
	cancel    context.CancelFunc
	shutdown  atomic.Bool
	mu        sync.RWMutex
}

func New(token string) *Channel {
	ch := &Channel{
		token:    token,
		incoming: make(chan *message.Message, 256),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "discord" }

func (c *Channel) Start(pctx context.Context) error {
	c.shutdown.Store(false)
	c.ensureIncoming()

	c.status.Store(int32(channel.StatusConnecting))

	session, err := discordgo.New("Bot " + c.token)
	if err != nil {
		c.setError(fmt.Sprintf("session create failed: %v", err))
		return fmt.Errorf("discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	session.AddHandler(c.onMessageCreate)
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		c.botID = r.User.ID
		c.status.Store(int32(channel.StatusConnected))
		slog.Info("discord connected", "bot", r.User.Username, "guilds", len(r.Guilds))
	})

	session.AddHandler(func(s *discordgo.Session, d *discordgo.Disconnect) {
		slog.Warn("discord disconnected")
		c.status.Store(int32(channel.StatusDisconnected))
	})

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Resumed) {
		slog.Info("discord resumed")
		c.status.Store(int32(channel.StatusConnected))
	})

	if err := session.Open(); err != nil {
		c.setError(fmt.Sprintf("open failed: %v", err))
		return fmt.Errorf("discord open: %w", err)
	}

	c.session = session
	c.startedAt = time.Now()

	ctx, cancel := context.WithCancel(pctx)
	c.cancel = cancel

	<-ctx.Done()
	if c.shutdown.Load() || pctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("discord: session ended")
}

func (c *Channel) Stop() error {
	c.shutdown.Store(true)
	if c.cancel != nil {
		c.cancel()
	}
	if c.session != nil {
		c.session.Close()
	}
	c.session = nil
	c.status.Store(int32(channel.StatusDisconnected))
	c.mu.Lock()
	if c.incoming != nil {
		close(c.incoming)
	}
	c.incoming = make(chan *message.Message, 256)
	c.mu.Unlock()
	return nil
}

func (c *Channel) Send(ctx context.Context, msg *message.Message) error {
	if c.session == nil {
		return fmt.Errorf("discord not connected")
	}

	if _, err := c.session.ChannelMessageSend(msg.To, msg.Text); err != nil {
		c.setError(err.Error())
		return fmt.Errorf("discord send: %w", err)
	}

	c.msgOut.Add(1)
	return nil
}

func (c *Channel) Receive() <-chan *message.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incoming
}

func (c *Channel) ensureIncoming() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.incoming == nil {
		c.incoming = make(chan *message.Message, 256)
	}
}

func (c *Channel) Health() channel.Health {
	c.mu.RLock()
	lastErr := c.lastErr
	c.mu.RUnlock()

	var uptime int64
	if !c.startedAt.IsZero() {
		uptime = int64(time.Since(c.startedAt).Seconds())
	}

	return channel.Health{
		Name:       "discord",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

func (c *Channel) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == c.botID || m.Author.Bot {
		return
	}

	msg := &message.Message{
		ID:        uuid.New().String(),
		Channel:   "discord",
		ChannelID: m.ChannelID,
		SessionID: m.ChannelID,
		From:      m.Author.Username,
		To:        c.botID,
		Text:      m.Content,
		Direction: message.Inbound,
		Timestamp: m.Timestamp,
		Metadata: map[string]string{
			"guild_id":   m.GuildID,
			"message_id": m.ID,
			"author_id":  m.Author.ID,
		},
	}

	c.msgIn.Add(1)
	c.incoming <- msg
}

func (c *Channel) setError(err string) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.status.Store(int32(channel.StatusError))
}
