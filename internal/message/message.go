package message

import "time"

type Direction int

const (
	Inbound  Direction = iota // from external channel to gateway
	Outbound                  // from gateway to external channel
)

type Message struct {
	ID        string    `json:"id"`
	Channel   string    `json:"channel"`
	ChannelID string    `json:"channel_id"`
	SessionID string    `json:"session_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Text      string    `json:"text"`
	Direction Direction `json:"direction"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Event struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

const (
	EventMessageReceived = "message.received"
	EventMessageSent     = "message.sent"
	EventChannelOnline   = "channel.online"
	EventChannelOffline  = "channel.offline"
	EventChannelError    = "channel.error"
	EventAgentResponse   = "agent.response"
	EventAgentThinking   = "agent.thinking"
	EventHealthStatus    = "health.status"
)
