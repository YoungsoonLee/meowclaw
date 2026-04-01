package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

var errUpdatesClosed = errors.New("updates channel closed")

type Channel struct {
	token     string
	bot       *tgbotapi.BotAPI
	incoming  chan *message.Message
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	lastErr   string
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

func (c *Channel) Name() string { return "telegram" }

func (c *Channel) Start(pctx context.Context) error {
	c.shutdown.Store(false)
	c.ensureIncoming()

	c.status.Store(int32(channel.StatusConnecting))

	bot, err := tgbotapi.NewBotAPI(c.token)
	if err != nil {
		c.setError(fmt.Sprintf("auth failed: %v", err))
		return fmt.Errorf("telegram auth: %w", err)
	}

	c.bot = bot
	c.startedAt = time.Now()
	c.status.Store(int32(channel.StatusConnected))
	slog.Info("telegram connected", "bot", bot.Self.UserName)

	ctx, cancel := context.WithCancel(pctx)
	c.cancel = cancel

	var updatesDead atomic.Bool
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					updatesDead.Store(true)
					c.setError("updates channel closed")
					cancel()
					return
				}
				c.handleUpdate(update)
			}
		}
	}()

	<-ctx.Done()
	if c.shutdown.Load() || pctx.Err() != nil {
		return nil
	}
	if updatesDead.Load() {
		return fmt.Errorf("telegram: %w", errUpdatesClosed)
	}
	return nil
}

func (c *Channel) Stop() error {
	c.shutdown.Store(true)
	if c.cancel != nil {
		c.cancel()
	}
	if c.bot != nil {
		c.bot.StopReceivingUpdates()
	}
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
	if c.bot == nil {
		return fmt.Errorf("telegram not connected")
	}

	chatID, err := strconv.ParseInt(msg.To, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat id %q: %w", msg.To, err)
	}

	tgMsg := tgbotapi.NewMessage(chatID, msg.Text)
	if _, err := c.bot.Send(tgMsg); err != nil {
		c.setError(err.Error())
		return fmt.Errorf("telegram send: %w", err)
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
		Name:       "telegram",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

func (c *Channel) handleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	msg := &message.Message{
		ID:        uuid.New().String(),
		Channel:   "telegram",
		ChannelID: strconv.FormatInt(update.Message.Chat.ID, 10),
		SessionID: strconv.FormatInt(update.Message.Chat.ID, 10),
		From:      update.Message.From.UserName,
		To:        c.bot.Self.UserName,
		Text:      update.Message.Text,
		Direction: message.Inbound,
		Timestamp: time.Unix(int64(update.Message.Date), 0),
		Metadata: map[string]string{
			"chat_type":  update.Message.Chat.Type,
			"message_id": strconv.Itoa(update.Message.MessageID),
		},
	}

	if update.Message.From != nil {
		msg.Metadata["first_name"] = update.Message.From.FirstName
		msg.Metadata["last_name"] = update.Message.From.LastName
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
