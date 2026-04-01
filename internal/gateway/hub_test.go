package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/message"
)

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub(0)
	go hub.Run()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
		id:   "test-client",
	}

	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Errorf("client count = %d, want 1", hub.ClientCount())
	}

	hub.unregister <- c
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("client count after unregister = %d, want 0", hub.ClientCount())
	}
}

func TestHubBroadcast(t *testing.T) {
	hub := NewHub(0)
	go hub.Run()

	c1 := &Client{hub: hub, send: make(chan []byte, 256), id: "c1"}
	c2 := &Client{hub: hub, send: make(chan []byte, 256), id: "c2"}

	hub.register <- c1
	hub.register <- c2
	time.Sleep(50 * time.Millisecond)

	hub.Broadcast(&message.Event{
		Type:    message.EventChannelOnline,
		Payload: map[string]string{"channel": "test"},
	})

	time.Sleep(50 * time.Millisecond)

	for _, c := range []*Client{c1, c2} {
		select {
		case data := <-c.send:
			var event message.Event
			if err := json.Unmarshal(data, &event); err != nil {
				t.Errorf("unmarshal: %v", err)
			}
			if event.Type != message.EventChannelOnline {
				t.Errorf("event type = %q, want channel.online", event.Type)
			}
		default:
			t.Errorf("client %s did not receive broadcast", c.id)
		}
	}
}

func TestHubInboundForwardsToAgentInbox(t *testing.T) {
	hub := NewHub(0)
	go hub.Run()

	msg := &message.Message{
		ID:      "inbound-1",
		Channel: "telegram",
		Text:    "hello agent",
	}

	hub.inbound <- msg
	time.Sleep(50 * time.Millisecond)

	select {
	case received := <-hub.agentInbox:
		if received.ID != "inbound-1" {
			t.Errorf("agent inbox got id = %q, want inbound-1", received.ID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("agent inbox did not receive the message")
	}
}

func TestHubSendCommand(t *testing.T) {
	hub := NewHub(0)
	go hub.Run()

	c := &Client{hub: hub, send: make(chan []byte, 256), id: "sender"}
	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	payload, _ := json.Marshal(message.Message{
		Channel: "telegram",
		To:      "chat-1",
		Text:    "outgoing msg",
	})

	hub.command <- &clientCommand{
		client: c,
		cmd: ClientCommand{
			Action:  "send",
			Payload: payload,
		},
	}

	select {
	case msg := <-hub.outbound:
		if msg.Text != "outgoing msg" {
			t.Errorf("outbound text = %q, want 'outgoing msg'", msg.Text)
		}
		if msg.Direction != message.Outbound {
			t.Errorf("direction = %d, want Outbound", msg.Direction)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("outbound channel did not receive the message")
	}
}

func TestHubPingCommand(t *testing.T) {
	hub := NewHub(0)
	go hub.Run()

	c := &Client{hub: hub, send: make(chan []byte, 256), id: "pinger"}
	hub.register <- c
	time.Sleep(50 * time.Millisecond)

	hub.command <- &clientCommand{
		client: c,
		cmd:    ClientCommand{Action: "ping"},
	}

	select {
	case data := <-c.send:
		var resp map[string]string
		json.Unmarshal(data, &resp)
		if resp["type"] != "pong" {
			t.Errorf("expected pong, got %v", resp)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("did not receive pong response")
	}
}
