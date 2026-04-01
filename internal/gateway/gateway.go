package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/config"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// MessageHandler is called for every inbound message. The returned message
// (if non-nil) is automatically routed back to the originating channel.
type MessageHandler func(ctx context.Context, msg *message.Message) *message.Message

// StreamHandler is like MessageHandler but receives a callback to deliver
// streaming deltas to WebSocket clients in real-time.
type StreamHandler func(ctx context.Context, msg *message.Message, onChunk func(delta string)) *message.Message

type Gateway struct {
	cfg           *config.Config
	hub           *Hub
	channels      map[string]channel.Channel
	handler       MessageHandler
	streamHandler StreamHandler
	server        *http.Server
	startAt       time.Time
	msgIn         atomic.Int64
	msgOut        atomic.Int64
	mu            sync.RWMutex

	upgrader websocket.Upgrader
}

func New(cfg *config.Config) *Gateway {
	return &Gateway{
		cfg:      cfg,
		hub:      NewHub(maxInputRunesForAgent(cfg.Agent)),
		channels: make(map[string]channel.Channel),
	}
}

func (g *Gateway) RegisterChannel(ch channel.Channel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.channels[ch.Name()] = ch
}

func (g *Gateway) OnMessage(h MessageHandler) {
	g.handler = h
}

func (g *Gateway) OnStream(h StreamHandler) {
	g.streamHandler = h
}

func (g *Gateway) Start(ctx context.Context) error {
	g.startAt = time.Now()

	g.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			ok := g.websocketOriginAllowed(r)
			if !ok {
				slog.Warn("websocket origin rejected", "origin", r.Header.Get("Origin"))
			}
			return ok
		},
	}
	if bindAddressExposesLAN(g.cfg.Gateway.Host) {
		slog.Warn("gateway listens on a non-loopback host — prefer reverse proxy + TLS; never expose raw to the internet without gateway.api_token")
		if !g.gatewayAuthRequired() {
			slog.Warn("gateway.api_token is unset — /api/send and /ws accept unauthenticated requests on this interface")
		}
	}
	if g.gatewayAuthRequired() {
		slog.Info("gateway API token enabled — add ?token=... to the dashboard URL for WebSocket; use Authorization: Bearer for HTTP API")
	}

	go g.hub.Run()
	go g.routeOutbound(ctx)
	go g.processInbound(ctx)

	// start all registered channels
	for name, ch := range g.channels {
		slog.Info("starting channel", "name", name)
		go func(name string, ch channel.Channel) {
			if err := ch.Start(ctx); err != nil {
				slog.Error("channel start failed", "name", name, "error", err)
				g.hub.Broadcast(&message.Event{
					Type: message.EventChannelError,
					Payload: map[string]string{
						"channel": name,
						"error":   err.Error(),
					},
				})
				return
			}
			g.hub.Broadcast(&message.Event{
				Type:    message.EventChannelOnline,
				Payload: map[string]string{"channel": name},
			})

			// pump channel messages into hub
			for msg := range ch.Receive() {
				g.msgIn.Add(1)
				g.hub.Inbound() <- msg
			}
		}(name, ch)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", g.handleWebSocket)
	mux.HandleFunc("/api/health", g.handleHealth)
	mux.HandleFunc("/api/channels", func(w http.ResponseWriter, r *http.Request) {
		if !g.gatewayAuthOK(w, r, false) {
			return
		}
		g.handleChannels(w, r)
	})
	mux.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		if !g.gatewayAuthOK(w, r, false) {
			return
		}
		g.handleSend(w, r)
	})
	mux.Handle("/", http.FileServer(http.Dir("web/static")))

	addr := fmt.Sprintf("%s:%d", g.cfg.Gateway.Host, g.cfg.Gateway.Port)
	g.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	slog.Info("gateway listening", "addr", addr)
	if err := g.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("gateway listen: %w", err)
	}
	return nil
}

func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for name, ch := range g.channels {
		slog.Info("stopping channel", "name", name)
		if err := ch.Stop(); err != nil {
			slog.Error("channel stop error", "name", name, "error", err)
		}
	}
	return g.server.Shutdown(ctx)
}

func (g *Gateway) processInbound(ctx context.Context) {
	if g.handler == nil && g.streamHandler == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-g.hub.agentInbox:
			go func(m *message.Message) {
				var reply *message.Message

				if g.streamHandler != nil {
					replyID := m.ID + "-reply"

					onChunk := func(delta string) {
						g.hub.Broadcast(&message.Event{
							Type: message.EventAgentStream,
							Payload: &message.StreamChunk{
								SessionID: m.SessionID,
								Channel:   m.Channel,
								ChannelID: m.ChannelID,
								Delta:     delta,
								MessageID: replyID,
							},
						})
					}

					reply = g.streamHandler(ctx, m, onChunk)

					if reply != nil {
						g.hub.Broadcast(&message.Event{
							Type: message.EventAgentStreamEnd,
							Payload: &message.StreamChunk{
								SessionID: m.SessionID,
								Channel:   m.Channel,
								ChannelID: m.ChannelID,
								MessageID: replyID,
							},
						})
					}
				} else {
					reply = g.handler(ctx, m)
				}

				if reply != nil {
					reply.Channel = m.Channel
					if reply.To == "" {
						reply.To = m.ChannelID
					}
					g.hub.Broadcast(&message.Event{
						Type:    message.EventAgentResponse,
						Payload: reply,
					})
					g.hub.outbound <- reply
				}
			}(msg)
		}
	}
}

func (g *Gateway) routeOutbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-g.hub.Outbound():
			g.mu.RLock()
			ch, ok := g.channels[msg.Channel]
			g.mu.RUnlock()
			if !ok {
				slog.Warn("unknown target channel", "channel", msg.Channel)
				continue
			}
			if err := ch.Send(ctx, msg); err != nil {
				slog.Error("send failed", "channel", msg.Channel, "error", err)
				continue
			}
			g.msgOut.Add(1)
			g.hub.Broadcast(&message.Event{
				Type:    message.EventMessageSent,
				Payload: msg,
			})
		}
	}
}

func (g *Gateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !g.gatewayAuthOK(w, r, true) {
		return
	}
	conn, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	clientID := uuid.New().String()[:8]
	client := newClient(g.hub, conn, clientID)
	g.hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	channels := make([]channel.Health, 0, len(g.channels))
	for _, ch := range g.channels {
		channels = append(channels, ch.Health())
	}
	g.mu.RUnlock()

	status := map[string]interface{}{
		"status":       "ok",
		"uptime":       int64(time.Since(g.startAt).Seconds()),
		"clients":      g.hub.ClientCount(),
		"messages_in":  g.msgIn.Load(),
		"messages_out": g.msgOut.Load(),
		"channels":     channels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (g *Gateway) handleChannels(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	channels := make([]channel.Health, 0, len(g.channels))
	for _, ch := range g.channels {
		channels = append(channels, ch.Health())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

func (g *Gateway) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, g.effectiveMaxAPIBodyBytes())
	var msg message.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	maxR := g.effectiveMaxInputRunes()
	if n := utf8.RuneCountInString(msg.Text); n > maxR {
		http.Error(w, fmt.Sprintf("text too large (%d runes, max %d)", n, maxR), http.StatusRequestEntityTooLarge)
		return
	}

	msg.Direction = message.Outbound
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	g.hub.outbound <- &msg

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "queued", "id": msg.ID})
}
