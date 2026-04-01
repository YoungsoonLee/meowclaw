package gateway

import (
	"encoding/json"
	"log/slog"
	"sync"
	"unicode/utf8"

	"github.com/YoungsoonLee/meowclaw/internal/config"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	command    chan *clientCommand
	inbound    chan *message.Message // messages from channels
	outbound   chan *message.Message // messages to channels
	agentInbox chan *message.Message // copy of inbound for agent processing
	mu         sync.RWMutex

	maxSendTextRunes int
}

func NewHub(maxSendTextRunes int) *Hub {
	if maxSendTextRunes <= 0 {
		maxSendTextRunes = config.DefaultMaxInputRunes
	}
	return &Hub{
		clients:          make(map[*Client]bool),
		broadcast:        make(chan []byte, 256),
		register:         make(chan *Client),
		unregister:       make(chan *Client),
		command:          make(chan *clientCommand, 256),
		inbound:          make(chan *message.Message, 256),
		outbound:         make(chan *message.Message, 256),
		agentInbox:       make(chan *message.Message, 256),
		maxSendTextRunes: maxSendTextRunes,
	}
}

func (h *Hub) Inbound() chan<- *message.Message {
	return h.inbound
}

func (h *Hub) Outbound() <-chan *message.Message {
	return h.outbound
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Broadcast(event *message.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal broadcast event", "error", err)
		return
	}
	h.broadcast <- data
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			slog.Info("client connected", "id", client.id, "total", h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			slog.Info("client disconnected", "id", client.id, "total", h.ClientCount())

		case data := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()

		case msg := <-h.inbound:
			event := &message.Event{
				Type:    message.EventMessageReceived,
				Payload: msg,
			}
			h.Broadcast(event)

			// forward to agent for processing
			select {
			case h.agentInbox <- msg:
			default:
				slog.Warn("agent inbox full, dropping message", "id", msg.ID)
			}

		case cmd := <-h.command:
			h.handleCommand(cmd)
		}
	}
}

func (h *Hub) handleCommand(cc *clientCommand) {
	switch cc.cmd.Action {
	case "send":
		var msg message.Message
		if err := json.Unmarshal(cc.cmd.Payload, &msg); err != nil {
			slog.Warn("invalid send payload", "client", cc.client.id, "error", err)
			return
		}
		if n := utf8.RuneCountInString(msg.Text); n > h.maxSendTextRunes {
			slog.Warn("ws send rejected: text too long", "client", cc.client.id, "runes", n, "max", h.maxSendTextRunes)
			return
		}
		msg.Direction = message.Outbound
		h.outbound <- &msg

	case "ping":
		resp, _ := json.Marshal(map[string]string{"type": "pong"})
		cc.client.send <- resp

	default:
		slog.Warn("unknown command", "action", cc.cmd.Action, "client", cc.client.id)
	}
}
