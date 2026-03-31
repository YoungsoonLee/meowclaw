package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/agent/provider"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

const defaultSystemPrompt = `You are MeowClaw, a helpful personal AI assistant. Be concise, friendly, and accurate. If you don't know something, say so.`

type Session struct {
	ID        string
	History   []provider.ChatMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Agent struct {
	provider     provider.Provider
	sessions     map[string]*Session
	systemPrompt string
	maxHistory   int
	mu           sync.RWMutex

	// optional memory store
	memoryStore MemoryStore
}

type MemoryStore interface {
	Save(ctx context.Context, sessionID string, role string, content string) error
	LoadHistory(ctx context.Context, sessionID string, limit int) ([]provider.ChatMessage, error)
	Search(ctx context.Context, query string, limit int) ([]string, error)
}

func New(p provider.Provider, opts ...Option) *Agent {
	a := &Agent{
		provider:     p,
		sessions:     make(map[string]*Session),
		systemPrompt: defaultSystemPrompt,
		maxHistory:   50,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

type Option func(*Agent)

func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) { a.systemPrompt = prompt }
}

func WithMaxHistory(n int) Option {
	return func(a *Agent) { a.maxHistory = n }
}

func WithMemory(store MemoryStore) Option {
	return func(a *Agent) { a.memoryStore = store }
}

func (a *Agent) Process(ctx context.Context, msg *message.Message) (*message.Message, error) {
	session := a.getOrCreateSession(msg.SessionID)

	// persist user message
	a.appendMessage(session, provider.ChatMessage{
		Role:    provider.RoleUser,
		Content: msg.Text,
	})

	if a.memoryStore != nil {
		_ = a.memoryStore.Save(ctx, msg.SessionID, string(provider.RoleUser), msg.Text)
	}

	// build request with conversation history
	req := &provider.ChatRequest{
		Messages: a.buildMessages(session),
	}

	slog.Debug("agent processing", "session", msg.SessionID, "history_len", len(session.History))

	resp, err := a.provider.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider chat: %w", err)
	}

	// persist assistant response
	a.appendMessage(session, provider.ChatMessage{
		Role:    provider.RoleAssistant,
		Content: resp.Content,
	})

	if a.memoryStore != nil {
		_ = a.memoryStore.Save(ctx, msg.SessionID, string(provider.RoleAssistant), resp.Content)
	}

	slog.Info("agent response",
		"session", msg.SessionID,
		"model", resp.Model,
		"input_tokens", resp.InputTokens,
		"output_tokens", resp.OutputTokens,
	)

	return &message.Message{
		ID:        msg.ID + "-reply",
		Channel:   msg.Channel,
		ChannelID: msg.ChannelID,
		SessionID: msg.SessionID,
		From:      "meowclaw",
		To:        msg.From,
		Text:      resp.Content,
		Direction: message.Outbound,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"model":         resp.Model,
			"input_tokens":  fmt.Sprintf("%d", resp.InputTokens),
			"output_tokens": fmt.Sprintf("%d", resp.OutputTokens),
			"reply_to":      msg.ChannelID,
		},
	}, nil
}

func (a *Agent) ResetSession(sessionID string) {
	a.mu.Lock()
	delete(a.sessions, sessionID)
	a.mu.Unlock()
}

func (a *Agent) getOrCreateSession(id string) *Session {
	a.mu.Lock()
	defer a.mu.Unlock()

	if s, ok := a.sessions[id]; ok {
		s.UpdatedAt = time.Now()
		return s
	}

	s := &Session{
		ID:        id,
		History:   make([]provider.ChatMessage, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	a.sessions[id] = s
	return s
}

func (a *Agent) appendMessage(session *Session, msg provider.ChatMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	session.History = append(session.History, msg)
	session.UpdatedAt = time.Now()

	// trim old messages to keep within limit
	if len(session.History) > a.maxHistory {
		session.History = session.History[len(session.History)-a.maxHistory:]
	}
}

func (a *Agent) buildMessages(session *Session) []provider.ChatMessage {
	a.mu.RLock()
	defer a.mu.RUnlock()

	msgs := make([]provider.ChatMessage, 0, len(session.History)+1)
	msgs = append(msgs, provider.ChatMessage{
		Role:    provider.RoleSystem,
		Content: a.systemPrompt,
	})
	msgs = append(msgs, session.History...)
	return msgs
}
