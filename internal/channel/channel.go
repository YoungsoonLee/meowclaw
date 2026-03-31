package channel

import (
	"context"

	"github.com/YoungsoonLee/meowclaw/internal/message"
)

type Status int

const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

type Health struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Uptime    int64  `json:"uptime_seconds"`
	MessageIn int64  `json:"messages_in"`
	MessageOut int64 `json:"messages_out"`
	LastError  string `json:"last_error,omitempty"`
}

// Channel is the interface every messaging platform must implement.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, msg *message.Message) error
	Receive() <-chan *message.Message
	Health() Health
}
