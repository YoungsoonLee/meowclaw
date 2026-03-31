package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/agent/provider"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// mockProvider returns a canned response for testing.
type mockProvider struct {
	response string
	err      error
	calls    int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &provider.ChatResponse{
		Content:      m.response,
		Model:        "mock-model",
		InputTokens:  10,
		OutputTokens: 5,
		FinishReason: "stop",
	}, nil
}

func TestProcessReturnsReply(t *testing.T) {
	mock := &mockProvider{response: "Hello human!"}
	ag := New(mock)

	msg := &message.Message{
		ID:        "msg-1",
		Channel:   "telegram",
		ChannelID: "chat-123",
		SessionID: "sess-1",
		From:      "user1",
		Text:      "Hi there",
		Direction: message.Inbound,
		Timestamp: time.Now(),
	}

	reply, err := ag.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if reply.Text != "Hello human!" {
		t.Errorf("reply text = %q, want 'Hello human!'", reply.Text)
	}
	if reply.Channel != "telegram" {
		t.Errorf("reply channel = %q, want telegram", reply.Channel)
	}
	if reply.From != "meowclaw" {
		t.Errorf("reply from = %q, want meowclaw", reply.From)
	}
	if reply.Direction != message.Outbound {
		t.Errorf("reply direction = %d, want Outbound", reply.Direction)
	}
	if mock.calls != 1 {
		t.Errorf("provider called %d times, want 1", mock.calls)
	}
}

func TestProcessProviderError(t *testing.T) {
	mock := &mockProvider{err: fmt.Errorf("rate limited")}
	ag := New(mock)

	msg := &message.Message{
		ID:        "msg-err",
		SessionID: "sess-err",
		Text:      "test",
		Timestamp: time.Now(),
	}

	_, err := ag.Process(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

func TestSessionManagement(t *testing.T) {
	callCount := 0
	mock := &mockProvider{response: "ok"}

	ag := New(mock)

	// first message creates a session
	msg1 := &message.Message{ID: "1", SessionID: "sess-A", Text: "first", Timestamp: time.Now()}
	ag.Process(context.Background(), msg1)

	// second message reuses the session
	msg2 := &message.Message{ID: "2", SessionID: "sess-A", Text: "second", Timestamp: time.Now()}
	ag.Process(context.Background(), msg2)

	_ = callCount

	ag.mu.RLock()
	session, exists := ag.sessions["sess-A"]
	ag.mu.RUnlock()

	if !exists {
		t.Fatal("session sess-A not found")
	}
	// 2 user messages + 2 assistant replies = 4
	if len(session.History) != 4 {
		t.Errorf("session history length = %d, want 4", len(session.History))
	}
}

func TestResetSession(t *testing.T) {
	mock := &mockProvider{response: "ok"}
	ag := New(mock)

	msg := &message.Message{ID: "1", SessionID: "sess-reset", Text: "hello", Timestamp: time.Now()}
	ag.Process(context.Background(), msg)

	ag.ResetSession("sess-reset")

	ag.mu.RLock()
	_, exists := ag.sessions["sess-reset"]
	ag.mu.RUnlock()

	if exists {
		t.Error("session should have been deleted after reset")
	}
}

func TestMaxHistoryTrimming(t *testing.T) {
	mock := &mockProvider{response: "r"}
	ag := New(mock, WithMaxHistory(6))

	for i := 0; i < 10; i++ {
		msg := &message.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			SessionID: "sess-trim",
			Text:      fmt.Sprintf("message %d", i),
			Timestamp: time.Now(),
		}
		ag.Process(context.Background(), msg)
	}

	ag.mu.RLock()
	session := ag.sessions["sess-trim"]
	histLen := len(session.History)
	ag.mu.RUnlock()

	if histLen > 6 {
		t.Errorf("history length = %d, should be <= 6 (maxHistory)", histLen)
	}
}

func TestCustomSystemPrompt(t *testing.T) {
	mock := &mockProvider{response: "ok"}
	ag := New(mock, WithSystemPrompt("You are a pirate."))

	if ag.systemPrompt != "You are a pirate." {
		t.Errorf("system prompt = %q, want 'You are a pirate.'", ag.systemPrompt)
	}
}
