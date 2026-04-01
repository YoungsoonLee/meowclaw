package slack

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

type Channel struct {
	botToken string
	appToken string

	api    *goslack.Client
	socket *socketmode.Client

	incoming  chan *message.Message
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	lastErr   string
	botUserID string
	cancel    context.CancelFunc
	shutdown  atomic.Bool
	mu        sync.RWMutex
}

func New(botToken, appToken string) *Channel {
	ch := &Channel{
		botToken: botToken,
		appToken: appToken,
		incoming: make(chan *message.Message, 256),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(pctx context.Context) error {
	c.shutdown.Store(false)
	c.ensureIncoming()

	c.status.Store(int32(channel.StatusConnecting))

	c.api = goslack.New(
		c.botToken,
		goslack.OptionAppLevelToken(c.appToken),
	)

	authResp, err := c.api.AuthTest()
	if err != nil {
		c.setError(fmt.Sprintf("auth test failed: %v", err))
		return fmt.Errorf("slack auth: %w", err)
	}
	c.botUserID = authResp.UserID
	slog.Info("slack authenticated", "bot", authResp.User, "team", authResp.Team)

	c.socket = socketmode.New(c.api)

	ctx, cancel := context.WithCancel(pctx)
	c.cancel = cancel
	c.startedAt = time.Now()

	go c.handleEvents(ctx)

	slog.Info("slack socket mode starting")
	err = c.socket.RunContext(ctx)
	if c.shutdown.Load() || pctx.Err() != nil {
		return nil
	}
	if err != nil {
		c.setError(fmt.Sprintf("socket mode error: %v", err))
		return fmt.Errorf("slack socket mode: %w", err)
	}
	return fmt.Errorf("slack socket mode stopped")
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
	if c.api == nil {
		return fmt.Errorf("slack not connected")
	}

	_, _, err := c.api.PostMessageContext(ctx, msg.To, goslack.MsgOptionText(msg.Text, false))
	if err != nil {
		c.setError(err.Error())
		return fmt.Errorf("slack send: %w", err)
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
		Name:       "slack",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

func (c *Channel) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-c.socket.Events:
			if !ok {
				return
			}
			c.processEvent(evt)
		}
	}
}

func (c *Channel) processEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		c.status.Store(int32(channel.StatusConnecting))
		slog.Debug("slack connecting")

	case socketmode.EventTypeConnected:
		c.status.Store(int32(channel.StatusConnected))
		slog.Info("slack connected via socket mode")

	case socketmode.EventTypeConnectionError:
		c.setError("connection error")
		slog.Warn("slack connection error, will retry")

	case socketmode.EventTypeEventsAPI:
		apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		c.socket.Ack(*evt.Request)
		c.handleAPIEvent(apiEvent)

	case socketmode.EventTypeHello:
		slog.Debug("slack hello received")

	default:
		// Ack any other envelope types to avoid Slack retries
		if evt.Request != nil {
			c.socket.Ack(*evt.Request)
		}
	}
}

func (c *Channel) handleAPIEvent(evt slackevents.EventsAPIEvent) {
	if evt.Type != slackevents.CallbackEvent {
		return
	}

	switch ev := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		if ev.User == c.botUserID || ev.User == "" || ev.BotID != "" {
			return
		}
		// Skip message subtypes (edits, deletes, etc.) — only handle plain messages
		if ev.SubType != "" {
			return
		}

		msg := &message.Message{
			ID:        uuid.New().String(),
			Channel:   "slack",
			ChannelID: ev.Channel,
			SessionID: ev.Channel,
			From:      ev.User,
			To:        c.botUserID,
			Text:      ev.Text,
			Direction: message.Inbound,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"thread_ts":  ev.ThreadTimeStamp,
				"message_ts": ev.TimeStamp,
			},
		}

		c.msgIn.Add(1)
		c.incoming <- msg

	case *slackevents.AppMentionEvent:
		if ev.User == c.botUserID || ev.User == "" {
			return
		}

		msg := &message.Message{
			ID:        uuid.New().String(),
			Channel:   "slack",
			ChannelID: ev.Channel,
			SessionID: ev.Channel,
			From:      ev.User,
			To:        c.botUserID,
			Text:      ev.Text,
			Direction: message.Inbound,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"thread_ts":  ev.ThreadTimeStamp,
				"message_ts": ev.TimeStamp,
				"type":       "mention",
			},
		}

		c.msgIn.Add(1)
		c.incoming <- msg
	}
}

func (c *Channel) setError(err string) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.status.Store(int32(channel.StatusError))
}
