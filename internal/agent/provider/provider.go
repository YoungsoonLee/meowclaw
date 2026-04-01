package provider

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ChatMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages    []ChatMessage `json:"messages"`
	Model       string        `json:"model,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type ChatResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	FinishReason string `json:"finish_reason"`
}

// StreamCallback is invoked for each text delta during streaming.
type StreamCallback func(delta string)

type Provider interface {
	Name() string
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}

// StreamingProvider extends Provider with streaming support. ChatStream calls
// onChunk for each text delta and returns the full ChatResponse when done.
type StreamingProvider interface {
	Provider
	ChatStream(ctx context.Context, req *ChatRequest, onChunk StreamCallback) (*ChatResponse, error)
}
