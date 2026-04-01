package webchat

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// WebChat is a pass-through channel for the built-in WebSocket UI.
// Messages arrive via the gateway WebSocket and are echoed back to the sender.
type Channel struct {
	incoming  chan *message.Message
	outgoing  map[string]chan *message.Message // sessionID -> response channel
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	mu        sync.RWMutex
}

func New() *Channel {
	ch := &Channel{
		incoming: make(chan *message.Message, 256),
		outgoing: make(map[string]chan *message.Message),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "webchat" }

func (c *Channel) Start(ctx context.Context) error {
	c.ensureIncoming()
	c.startedAt = time.Now()
	c.status.Store(int32(channel.StatusConnected))
	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error {
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
	c.msgOut.Add(1)
	c.mu.RLock()
	ch, ok := c.outgoing[msg.SessionID]
	c.mu.RUnlock()
	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
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
	var uptime int64
	if !c.startedAt.IsZero() {
		uptime = int64(time.Since(c.startedAt).Seconds())
	}

	return channel.Health{
		Name:       "webchat",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
	}
}

func (c *Channel) InjectMessage(msg *message.Message) {
	c.msgIn.Add(1)
	c.mu.RLock()
	ch := c.incoming
	c.mu.RUnlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *Channel) Subscribe(sessionID string) <-chan *message.Message {
	ch := make(chan *message.Message, 64)
	c.mu.Lock()
	c.outgoing[sessionID] = ch
	c.mu.Unlock()
	return ch
}

func (c *Channel) Unsubscribe(sessionID string) {
	c.mu.Lock()
	if ch, ok := c.outgoing[sessionID]; ok {
		close(ch)
		delete(c.outgoing, sessionID)
	}
	c.mu.Unlock()
}
