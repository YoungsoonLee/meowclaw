package kakao

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// Kakao i Open Builder webhook request structures.
// See: https://i.kakao.com/docs/skill-response-format

type skillRequest struct {
	Intent     skillIntent     `json:"intent"`
	UserReq    skillUserReq    `json:"userRequest"`
	Action     skillAction     `json:"action"`
}

type skillIntent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type skillUserReq struct {
	Timezone   string      `json:"timezone"`
	Block      skillBlock  `json:"block"`
	Utterance  string      `json:"utterance"`
	Lang       string      `json:"lang"`
	User       skillUser   `json:"user"`
}

type skillBlock struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type skillUser struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
}

type skillAction struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params"`
}

// skillResponse is the Kakao i Open Builder skill response format.
type skillResponse struct {
	Version  string           `json:"version"`
	Template skillTemplate    `json:"template"`
}

type skillTemplate struct {
	Outputs []skillOutput `json:"outputs"`
}

type skillOutput struct {
	SimpleText *simpleText `json:"simpleText,omitempty"`
}

type simpleText struct {
	Text string `json:"text"`
}

// Channel implements channel.Channel and channel.WebhookChannel for Kakao i Open Builder.
type Channel struct {
	incoming  chan *message.Message
	status    atomic.Int32
	startedAt time.Time
	msgIn     atomic.Int64
	msgOut    atomic.Int64
	lastErr   string
	cancel    context.CancelFunc
	shutdown  atomic.Bool
	mu        sync.RWMutex

	// pendingReplies stores agent replies keyed by session ID for synchronous webhook responses.
	pendingReplies sync.Map
}

// pendingReply is used to synchronously return agent replies within the webhook HTTP response.
type pendingReply struct {
	ch chan string
}

func New() *Channel {
	ch := &Channel{
		incoming: make(chan *message.Message, 256),
	}
	ch.status.Store(int32(channel.StatusDisconnected))
	return ch
}

func (c *Channel) Name() string { return "kakao" }

func (c *Channel) Start(pctx context.Context) error {
	c.shutdown.Store(false)
	c.ensureIncoming()

	c.startedAt = time.Now()
	c.status.Store(int32(channel.StatusConnected))
	slog.Info("kakao channel connected (webhook mode)")

	ctx, cancel := context.WithCancel(pctx)
	c.cancel = cancel

	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error {
	c.shutdown.Store(true)
	if c.cancel != nil {
		c.cancel()
	}
	c.status.Store(int32(channel.StatusDisconnected))
	c.mu.Lock()
	if c.incoming != nil {
		close(c.incoming)
	}
	c.incoming = make(chan *message.Message, 256)
	c.mu.Unlock()
	return nil
}

// Send delivers an agent reply. For Kakao, this is routed via pendingReplies
// so the webhook handler can return it synchronously.
func (c *Channel) Send(ctx context.Context, msg *message.Message) error {
	if v, ok := c.pendingReplies.LoadAndDelete(msg.To); ok {
		pr := v.(*pendingReply)
		select {
		case pr.ch <- msg.Text:
		default:
		}
		c.msgOut.Add(1)
		return nil
	}
	// No pending reply (timed out already) — log and drop.
	slog.Warn("kakao: no pending reply slot", "to", msg.To)
	return nil
}

func (c *Channel) Receive() <-chan *message.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incoming
}

func (c *Channel) Health() channel.Health {
	c.mu.RLock()
	lastErr := c.lastErr
	c.mu.RUnlock()

	var uptime int64
	if !c.startedAt.IsZero() {
		uptime = int64(time.Since(c.startedAt).Seconds())
	}

	return channel.Health{
		Name:       "kakao",
		Status:     channel.Status(c.status.Load()).String(),
		Uptime:     uptime,
		MessageIn:  c.msgIn.Load(),
		MessageOut: c.msgOut.Load(),
		LastError:  lastErr,
	}
}

// WebhookPath returns the HTTP path the gateway should register for this channel.
func (c *Channel) WebhookPath() string {
	return "/webhook/kakao"
}

// WebhookHTTPHandler returns the http.HandlerFunc that processes Kakao i Open Builder skill requests.
// Kakao requires a synchronous JSON response within 5 seconds.
func (c *Channel) WebhookHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req skillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("kakao webhook parse error", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		userID := req.UserReq.User.ID
		utterance := req.UserReq.Utterance

		if utterance == "" {
			c.respondText(w, "메시지를 입력해주세요.")
			return
		}

		// Create a pending reply slot so Send() can deliver the agent's answer.
		pr := &pendingReply{ch: make(chan string, 1)}
		c.pendingReplies.Store(userID, pr)

		msg := &message.Message{
			ID:        uuid.New().String(),
			Channel:   "kakao",
			ChannelID: userID,
			SessionID: userID,
			From:      userID,
			To:        "",
			Text:      utterance,
			Direction: message.Inbound,
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"intent_id":   req.Intent.ID,
				"intent_name": req.Intent.Name,
				"block_id":    req.UserReq.Block.ID,
				"user_type":   req.UserReq.User.Type,
			},
		}

		c.msgIn.Add(1)
		c.incoming <- msg

		// Wait for agent reply (Kakao allows up to 5 seconds).
		select {
		case reply := <-pr.ch:
			c.respondText(w, reply)
		case <-time.After(4500 * time.Millisecond):
			c.pendingReplies.Delete(userID)
			c.respondText(w, "처리 중입니다. 잠시 후 다시 시도해주세요.")
		}
	}
}

func (c *Channel) respondText(w http.ResponseWriter, text string) {
	resp := skillResponse{
		Version: "2.0",
		Template: skillTemplate{
			Outputs: []skillOutput{
				{SimpleText: &simpleText{Text: text}},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (c *Channel) ensureIncoming() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.incoming == nil {
		c.incoming = make(chan *message.Message, 256)
	}
}

func (c *Channel) setError(err string) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.status.Store(int32(channel.StatusError))
}
