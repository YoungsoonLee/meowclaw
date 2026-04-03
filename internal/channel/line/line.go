package line

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// Channel implements channel.Channel and channel.WebhookChannel for LINE Messaging API.
type Channel struct {
	channelSecret string
	accessToken   string

	bot       *messaging_api.MessagingApiAPI
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

func New(channelSecret, accessToken string) *Channel {
	ch := &Channel{
		channelSecret: channelSecret,
		accessToken:   accessToken,
		incoming:      make(chan *message.Message, 256),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "line" }

func (c *Channel) Start(pctx context.Context) error {
	c.shutdown.Store(false)
	c.ensureIncoming()

	c.status.Store(int32(channel.StatusConnecting))

	bot, err := messaging_api.NewMessagingApiAPI(c.accessToken)
	if err != nil {
		c.setError(fmt.Sprintf("LINE API init failed: %v", err))
		return fmt.Errorf("line api init: %w", err)
	}
	c.bot = bot

	c.startedAt = time.Now()
	c.status.Store(int32(channel.StatusConnected))
	slog.Info("line channel connected (webhook mode)")

	ctx, cancel := context.WithCancel(pctx)
	c.cancel = cancel

	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error {
	c.shutdown.Store(true)
	if c.cancel != nil {
		c.cancel()
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
		return fmt.Errorf("line not connected")
	}

	_, err := c.bot.PushMessage(
		&messaging_api.PushMessageRequest{
			To: msg.To,
			Messages: []messaging_api.MessageInterface{
				&messaging_api.TextMessage{Text: msg.Text},
			},
		},
		"",
	)
	if err != nil {
		c.setError(err.Error())
		return fmt.Errorf("line send: %w", err)
	}

	c.msgOut.Add(1)
	return nil
}

func (c *Channel) Receive() <-chan *message.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incoming
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
		Name:       "line",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

// WebhookPath returns the HTTP path the gateway should register for this channel.
func (c *Channel) WebhookPath() string {
	return "/webhook/line"
}

// WebhookHTTPHandler returns the http.HandlerFunc that processes LINE webhook callbacks.
func (c *Channel) WebhookHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cb, err := webhook.ParseRequest(c.channelSecret, r)
		if err != nil {
			slog.Warn("line webhook parse error", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		for _, event := range cb.Events {
			c.handleEvent(event)
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (c *Channel) handleEvent(event webhook.EventInterface) {
	switch e := event.(type) {
	case webhook.MessageEvent:
		switch m := e.Message.(type) {
		case webhook.TextMessageContent:
			var userID string
			if src, ok := e.Source.(webhook.UserSource); ok {
				userID = src.UserId
			}

			// Determine reply target: group/room ID or user ID
			replyTo := userID
			switch src := e.Source.(type) {
			case webhook.GroupSource:
				replyTo = src.GroupId
			case webhook.RoomSource:
				replyTo = src.RoomId
			}

			msg := &message.Message{
				ID:        uuid.New().String(),
				Channel:   "line",
				ChannelID: replyTo,
				SessionID: replyTo,
				From:      userID,
				To:        "",
				Text:      m.Text,
				Direction: message.Inbound,
				Timestamp: time.UnixMilli(e.Timestamp),
				Metadata: map[string]string{
					"reply_token": e.ReplyToken,
					"message_id":  m.Id,
				},
			}

			c.msgIn.Add(1)
			c.incoming <- msg
		}
	}
}

func (c *Channel) ensureIncoming() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.incoming == nil {
		c.incoming = make(chan *message.Message, 256)
	}
}

func (c *Channel) setError(err string) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.status.Store(int32(channel.StatusError))
}
