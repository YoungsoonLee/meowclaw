package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/message"
)

func TestParseChatCommand(t *testing.T) {
	tests := []struct {
		in, wantCmd, wantArg string
		ok                   bool
	}{
		{"/reset", "/reset", "", true},
		{"/RESET", "/reset", "", true},
		{"  /model  gpt-4o-mini  ", "/model", "gpt-4o-mini", true},
		{"/status", "/status", "", true},
		{"/new", "/new", "", true},
		{"hello", "", "", false},
		{"/unknown", "", "", false},
	}
	for _, tt := range tests {
		cmd, arg, ok := parseChatCommand(tt.in)
		if ok != tt.ok || cmd != tt.wantCmd || arg != tt.wantArg {
			t.Errorf("parseChatCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, cmd, arg, ok, tt.wantCmd, tt.wantArg, tt.ok)
		}
	}
}

func TestChatCommandResetNotInHistory(t *testing.T) {
	mock := &mockProvider{response: "bye"}
	ag := New(mock, WithRuntimeMeta("openai", "gpt-4o"))

	base := &message.Message{
		ID:        "1",
		SessionID: "sess-cmd",
		Text:      "/reset",
		Timestamp: time.Now(),
	}
	_, err := ag.Process(context.Background(), base)
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if mock.calls != 0 {
		t.Errorf("provider calls = %d, want 0 (command should not call LLM)", mock.calls)
	}

	_, err = ag.Process(context.Background(), &message.Message{
		ID:        "2",
		SessionID: "sess-cmd",
		Text:      "ping",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if mock.calls != 1 {
		t.Errorf("provider calls = %d, want 1", mock.calls)
	}

	ag.mu.RLock()
	s := ag.sessions["sess-cmd"]
	n := len(s.History)
	ag.mu.RUnlock()
	// Only "ping" user + assistant reply = 2
	if n != 2 {
		t.Errorf("history len = %d, want 2 (no /reset stored)", n)
	}
}

func TestChatCommandModelOverride(t *testing.T) {
	mock := &mockProvider{response: "ok"}
	ag := New(mock, WithRuntimeMeta("openai", "gpt-4o"))

	_, err := ag.Process(context.Background(), &message.Message{
		ID:        "a",
		SessionID: "sess-m",
		Text:      "/model claude-haiku",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = ag.Process(context.Background(), &message.Message{
		ID:        "b",
		SessionID: "sess-m",
		Text:      "hi",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mock.lastModel != "claude-haiku" {
		t.Errorf("ChatRequest.Model = %q, want claude-haiku", mock.lastModel)
	}
}

func TestChatCommandStatusIncludesMeta(t *testing.T) {
	mock := &mockProvider{response: "x"}
	ag := New(mock, WithRuntimeMeta("openai", "gpt-4o"))

	reply, err := ag.Process(context.Background(), &message.Message{
		ID:        "s",
		SessionID: "sess-s",
		Text:      "/status",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mock.calls != 0 {
		t.Error("status should not call LLM")
	}
	if reply == nil || !containsAll(reply.Text, []string{"openai", "gpt-4o", "sess-s"}) {
		t.Errorf("unexpected status reply: %q", reply.Text)
	}
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestProcessStreamChatCommandSkipsLLM(t *testing.T) {
	mock := &mockStreamProvider{chunks: []string{"x"}}
	ag := New(mock, WithRuntimeMeta("openai", "gpt-4o"))

	var chunks []string
	reply, err := ag.ProcessStream(context.Background(), &message.Message{
		ID:        "c",
		SessionID: "sess",
		Text:      "/status",
		Timestamp: time.Now(),
	}, func(d string) { chunks = append(chunks, d) })
	if err != nil {
		t.Fatal(err)
	}
	if reply == nil || !strings.Contains(reply.Text, "Provider") {
		t.Fatalf("expected status reply, got %+v", reply)
	}
	if mock.calls != 0 {
		t.Errorf("stream provider calls = %d, want 0", mock.calls)
	}
	if len(chunks) != 0 {
		t.Errorf("expected no stream chunks for command, got %v", chunks)
	}
}
