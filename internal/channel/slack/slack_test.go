package slack

import (
	"testing"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

var _ channel.Channel = (*Channel)(nil)

func TestSlackName(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	if ch.Name() != "slack" {
		t.Errorf("name = %q, want slack", ch.Name())
	}
}

func TestSlackHealthDefaults(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	h := ch.Health()

	if h.Name != "slack" {
		t.Errorf("health name = %q, want slack", h.Name)
	}
	if h.Status != "disconnected" {
		t.Errorf("status = %q, want disconnected", h.Status)
	}
	if h.MessageIn != 0 {
		t.Errorf("messages_in = %d, want 0", h.MessageIn)
	}
	if h.MessageOut != 0 {
		t.Errorf("messages_out = %d, want 0", h.MessageOut)
	}
}

func TestSlackReceiveChannel(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	recv := ch.Receive()
	if recv == nil {
		t.Error("Receive() returned nil channel")
	}
}

func TestSlackSetError(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	ch.setError("something broke")

	h := ch.Health()
	if h.Status != "error" {
		t.Errorf("status = %q, want error", h.Status)
	}
	if h.LastError != "something broke" {
		t.Errorf("last_error = %q, want 'something broke'", h.LastError)
	}
}

func TestSlackProcessEventConnectionStates(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")

	ch.processEvent(socketmode.Event{Type: socketmode.EventTypeConnecting})
	if channel.Status(ch.status.Load()) != channel.StatusConnecting {
		t.Error("expected connecting status")
	}

	ch.processEvent(socketmode.Event{Type: socketmode.EventTypeConnected})
	if channel.Status(ch.status.Load()) != channel.StatusConnected {
		t.Error("expected connected status")
	}

	ch.processEvent(socketmode.Event{Type: socketmode.EventTypeConnectionError})
	if channel.Status(ch.status.Load()) != channel.StatusError {
		t.Error("expected error status")
	}
}

func TestSlackHandleAPIEventMessage(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	ch.botUserID = "U_BOT"

	evt := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				User:    "U_HUMAN",
				Channel: "C_GENERAL",
				Text:    "hello meowclaw",
			},
		},
	}

	ch.handleAPIEvent(evt)

	select {
	case msg := <-ch.incoming:
		if msg.Channel != "slack" {
			t.Errorf("channel = %q, want slack", msg.Channel)
		}
		if msg.ChannelID != "C_GENERAL" {
			t.Errorf("channel_id = %q, want C_GENERAL", msg.ChannelID)
		}
		if msg.From != "U_HUMAN" {
			t.Errorf("from = %q, want U_HUMAN", msg.From)
		}
		if msg.Text != "hello meowclaw" {
			t.Errorf("text = %q, want 'hello meowclaw'", msg.Text)
		}
		if msg.Direction != message.Inbound {
			t.Errorf("direction = %v, want inbound", msg.Direction)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("did not receive message from handleAPIEvent")
	}

	h := ch.Health()
	if h.MessageIn != 1 {
		t.Errorf("messages_in = %d, want 1", h.MessageIn)
	}
}

func TestSlackIgnoresBotMessages(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	ch.botUserID = "U_BOT"

	// Message from the bot itself
	ch.handleAPIEvent(slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				User:    "U_BOT",
				Channel: "C_GENERAL",
				Text:    "my own message",
			},
		},
	})

	// Message with a bot ID (webhook/integration)
	ch.handleAPIEvent(slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				User:    "U_OTHER",
				BotID:   "B_123",
				Channel: "C_GENERAL",
				Text:    "bot message",
			},
		},
	})

	// Message with empty user
	ch.handleAPIEvent(slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				User:    "",
				Channel: "C_GENERAL",
				Text:    "ghost",
			},
		},
	})

	// Subtype message (edit, delete, etc.)
	ch.handleAPIEvent(slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.Message),
			Data: &slackevents.MessageEvent{
				User:    "U_HUMAN",
				SubType: "message_changed",
				Channel: "C_GENERAL",
				Text:    "edited",
			},
		},
	})

	select {
	case msg := <-ch.incoming:
		t.Errorf("should not receive any message, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// expected — no messages
	}

	if ch.msgIn.Load() != 0 {
		t.Errorf("messages_in = %d, want 0 (all should be filtered)", ch.msgIn.Load())
	}
}

func TestSlackHandleAPIEventMention(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	ch.botUserID = "U_BOT"

	evt := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Type: string(slackevents.AppMention),
			Data: &slackevents.AppMentionEvent{
				User:    "U_HUMAN",
				Channel: "C_DEV",
				Text:    "<@U_BOT> what is meowclaw?",
			},
		},
	}

	ch.handleAPIEvent(evt)

	select {
	case msg := <-ch.incoming:
		if msg.Text != "<@U_BOT> what is meowclaw?" {
			t.Errorf("text = %q", msg.Text)
		}
		if msg.Metadata["type"] != "mention" {
			t.Errorf("metadata type = %q, want mention", msg.Metadata["type"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("did not receive mention message")
	}
}

func TestSlackIgnoresNonCallbackEvents(t *testing.T) {
	ch := New("xoxb-fake", "xapp-fake")
	ch.botUserID = "U_BOT"

	ch.handleAPIEvent(slackevents.EventsAPIEvent{
		Type: "url_verification",
	})

	select {
	case msg := <-ch.incoming:
		t.Errorf("should not receive message for non-callback event, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}
