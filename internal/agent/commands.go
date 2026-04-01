package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// parseChatCommand returns (canonicalCmd, arg, true) when text is a known slash command.
// arg is everything after the first token (e.g. "/model gpt-4o" -> arg "gpt-4o").
func parseChatCommand(text string) (cmd string, arg string, ok bool) {
	line := strings.TrimSpace(text)
	if line == "" || line[0] != '/' {
		return "", "", false
	}
	sp := strings.IndexByte(line, ' ')
	var cmdPart string
	if sp < 0 {
		cmdPart = line
	} else {
		cmdPart = line[:sp]
		arg = strings.TrimSpace(line[sp+1:])
	}
	c := strings.ToLower(cmdPart)
	switch c {
	case "/new", "/reset", "/status", "/model":
		return c, arg, true
	default:
		return "", "", false
	}
}

func (a *Agent) handleChatCommand(_ context.Context, msg *message.Message, cmd, arg string) (*message.Message, error) {
	switch cmd {
	case "/new", "/reset":
		a.ResetSession(msg.SessionID)
		text := "Session cleared. Your next message starts a fresh conversation."
		if cmd == "/new" {
			text = "New conversation started. (History for this chat was cleared.)"
		}
		return a.commandReply(msg, text), nil

	case "/status":
		eff := a.effectiveModelForSession(msg.SessionID)
		pn := a.providerName
		if pn == "" {
			pn = a.provider.Name()
		}
		dm := a.defaultModel
		if dm == "" {
			dm = "(see provider default)"
		}
		sess := msg.SessionID
		if len(sess) > 48 {
			sess = sess[:45] + "..."
		}
		text := fmt.Sprintf(
			"MeowClaw\n• Provider: %s\n• Config default model: %s\n• This chat model: %s\n• Session: %s",
			pn, dm, eff, sess,
		)
		return a.commandReply(msg, text), nil

	case "/model":
		if arg == "" {
			eff := a.effectiveModelForSession(msg.SessionID)
			text := fmt.Sprintf(
				"Current model for this chat: %s\nSend `/model <name>` to switch (this chat only). `/new` or `/reset` clears the override.",
				eff,
			)
			return a.commandReply(msg, text), nil
		}
		a.setSessionModel(msg.SessionID, arg)
		text := fmt.Sprintf("Model for this chat set to: %s", arg)
		return a.commandReply(msg, text), nil

	default:
		return nil, fmt.Errorf("unknown command")
	}
}

func (a *Agent) commandReply(orig *message.Message, text string) *message.Message {
	return &message.Message{
		ID:        orig.ID + "-reply",
		Channel:   orig.Channel,
		ChannelID: orig.ChannelID,
		SessionID: orig.SessionID,
		From:      "meowclaw",
		To:        orig.From,
		Text:      text,
		Direction: message.Outbound,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"reply_to": orig.ChannelID,
			"command":  "true",
		},
	}
}

func (a *Agent) effectiveModelForSession(sessionID string) string {
	a.sessionModelMu.RLock()
	m := a.sessionModel[sessionID]
	a.sessionModelMu.RUnlock()
	if m != "" {
		return m
	}
	if a.defaultModel != "" {
		return a.defaultModel
	}
	return "(provider default)"
}

func (a *Agent) modelForRequest(sessionID string) string {
	a.sessionModelMu.RLock()
	m := a.sessionModel[sessionID]
	a.sessionModelMu.RUnlock()
	return m
}

func (a *Agent) setSessionModel(sessionID, model string) {
	a.sessionModelMu.Lock()
	defer a.sessionModelMu.Unlock()
	if a.sessionModel == nil {
		a.sessionModel = make(map[string]string)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		delete(a.sessionModel, sessionID)
		return
	}
	a.sessionModel[sessionID] = model
}
