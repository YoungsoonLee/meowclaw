package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

type Channel struct {
	dbPath    string
	client    *whatsmeow.Client
	incoming  chan *message.Message
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	lastErr   string
	cancel    context.CancelFunc
	mu        sync.RWMutex
}

func New(dbPath string) *Channel {
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".meowclaw", "whatsapp.db")
	}
	ch := &Channel{
		dbPath:   dbPath,
		incoming: make(chan *message.Message, 256),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "whatsapp" }

func (c *Channel) Start(ctx context.Context) error {
	c.status.Store(int32(channel.StatusConnecting))

	if err := os.MkdirAll(filepath.Dir(c.dbPath), 0755); err != nil {
		c.setError(err.Error())
		return fmt.Errorf("create db dir: %w", err)
	}

	dbLog := waLog.Noop
	container, err := sqlstore.New(ctx, "sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", c.dbPath), dbLog)
	if err != nil {
		c.setError(err.Error())
		return fmt.Errorf("whatsapp db: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		c.setError(err.Error())
		return fmt.Errorf("whatsapp device: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLog.Noop)
	c.client = client

	client.AddEventHandler(c.eventHandler)

	if client.Store.ID == nil {
		// Not logged in: show QR
		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			c.setError(err.Error())
			return fmt.Errorf("whatsapp connect: %w", err)
		}

		for evt := range qrChan {
			switch evt.Event {
			case "code":
				slog.Info("whatsapp: scan QR code to login")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stderr)
			case "login":
				slog.Info("whatsapp: login successful")
			case "timeout":
				c.setError("QR code timeout")
				return fmt.Errorf("whatsapp QR timeout")
			}
		}
	} else {
		if err := client.Connect(); err != nil {
			c.setError(err.Error())
			return fmt.Errorf("whatsapp connect: %w", err)
		}
	}

	c.startedAt = time.Now()
	c.status.Store(int32(channel.StatusConnected))
	slog.Info("whatsapp connected")

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// keepalive: send presence every 4 minutes to prevent session timeout
	go c.keepAlive(ctx)

	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.client != nil {
		c.client.Disconnect()
	}
	c.status.Store(int32(channel.StatusDisconnected))
	close(c.incoming)
	return nil
}

func (c *Channel) Send(ctx context.Context, msg *message.Message) error {
	if c.client == nil {
		return fmt.Errorf("whatsapp not connected")
	}

	jid, err := types.ParseJID(msg.To)
	if err != nil {
		return fmt.Errorf("invalid JID %q: %w", msg.To, err)
	}

	resp, err := c.client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(msg.Text),
	})
	if err != nil {
		c.setError(err.Error())
		return fmt.Errorf("whatsapp send: %w", err)
	}

	c.msgOut.Add(1)
	slog.Debug("whatsapp sent", "to", msg.To, "server_id", resp.ID)
	return nil
}

func (c *Channel) Receive() <-chan *message.Message {
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
		Name:       "whatsapp",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

func (c *Channel) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}

		text := ""
		if v.Message.GetConversation() != "" {
			text = v.Message.GetConversation()
		} else if v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}

		if text == "" {
			return
		}

		msg := &message.Message{
			ID:        uuid.New().String(),
			Channel:   "whatsapp",
			ChannelID: v.Info.Chat.String(),
			SessionID: v.Info.Chat.String(),
			From:      v.Info.Sender.String(),
			To:        "self",
			Text:      text,
			Direction: message.Inbound,
			Timestamp: v.Info.Timestamp,
			Metadata: map[string]string{
				"message_id": v.Info.ID,
				"push_name":  v.Info.PushName,
				"is_group":   fmt.Sprintf("%v", v.Info.IsGroup),
			},
		}
		c.msgIn.Add(1)
		c.incoming <- msg

	case *events.Connected:
		c.status.Store(int32(channel.StatusConnected))
		slog.Info("whatsapp: connected event")

	case *events.Disconnected:
		c.status.Store(int32(channel.StatusDisconnected))
		slog.Warn("whatsapp: disconnected event")

	case *events.LoggedOut:
		c.setError("logged out by server")
		slog.Error("whatsapp: logged out - credentials may need refresh")
	}
}

// keepAlive prevents WhatsApp from killing idle sessions (the #1 OpenClaw pain point).
func (c *Channel) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(4 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.client != nil && c.client.IsConnected() {
				if err := c.client.SendPresence(ctx, types.PresenceAvailable); err != nil {
					slog.Warn("whatsapp keepalive failed", "error", err)
				}
			}
		}
	}
}

func (c *Channel) setError(err string) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.status.Store(int32(channel.StatusError))
}
