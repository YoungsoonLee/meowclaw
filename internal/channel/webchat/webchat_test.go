package webchat

import (
	"context"
	"testing"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// compile-time interface check
var _ channel.Channel = (*Channel)(nil)

func TestWebChatName(t *testing.T) {
	ch := New()
	if ch.Name() != "webchat" {
		t.Errorf("name = %q, want webchat", ch.Name())
	}
}

func TestWebChatStartSetsConnected(t *testing.T) {
	ch := New()
	ctx, cancel := context.WithCancel(context.Background())

	go ch.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	health := ch.Health()
	if health.Status != "connected" {
		t.Errorf("status = %q, want connected", health.Status)
	}

	cancel()
}

func TestWebChatInjectAndReceive(t *testing.T) {
	ch := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ch.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	msg := &message.Message{
		ID:   "test-msg",
		Text: "hello from webchat",
	}

	ch.InjectMessage(msg)

	select {
	case received := <-ch.Receive():
		if received.ID != "test-msg" {
			t.Errorf("received id = %q, want test-msg", received.ID)
		}
		if received.Text != "hello from webchat" {
			t.Errorf("received text = %q", received.Text)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("did not receive injected message")
	}
}

func TestWebChatSubscribeAndSend(t *testing.T) {
	ch := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ch.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	sub := ch.Subscribe("sess-1")

	msg := &message.Message{
		ID:        "reply-1",
		SessionID: "sess-1",
		Text:      "AI response",
	}

	ch.Send(ctx, msg)

	select {
	case received := <-sub:
		if received.Text != "AI response" {
			t.Errorf("subscriber got %q, want 'AI response'", received.Text)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("subscriber did not receive message")
	}

	ch.Unsubscribe("sess-1")
}

func TestWebChatHealthCounters(t *testing.T) {
	ch := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ch.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	ch.InjectMessage(&message.Message{ID: "1", Text: "in"})
	<-ch.Receive()

	ch.Send(ctx, &message.Message{ID: "2", SessionID: "x", Text: "out"})

	health := ch.Health()
	if health.MessageIn != 1 {
		t.Errorf("messages_in = %d, want 1", health.MessageIn)
	}
	if health.MessageOut != 1 {
		t.Errorf("messages_out = %d, want 1", health.MessageOut)
	}
}
