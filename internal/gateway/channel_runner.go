package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/YoungsoonLee/meowclaw/internal/channel"
	"github.com/YoungsoonLee/meowclaw/internal/message"
)

// runChannel starts inbound pumping concurrently with Channel.Start and, for
// external bridges (not webchat), retries after errors or unexpected exit
// using exponential backoff.
func (g *Gateway) runChannel(parent context.Context, name string, ch channel.Channel) {
	if name == "webchat" {
		g.runWebchatChannel(parent, name, ch)
		return
	}
	g.runReconnectingChannel(parent, name, ch)
}

func (g *Gateway) runWebchatChannel(parent context.Context, name string, ch channel.Channel) {
	recvCtx, recvCancel := context.WithCancel(parent)
	recvDone := make(chan struct{})
	go g.pumpInbound(recvCtx, ch, recvDone)

	err := ch.Start(parent)
	recvCancel()
	<-recvDone

	if err != nil && parent.Err() == nil {
		slog.Error("channel start failed", "name", name, "error", err)
		g.hub.Broadcast(&message.Event{
			Type: message.EventChannelError,
			Payload: map[string]string{
				"channel": name,
				"error":   err.Error(),
			},
		})
	}
}

func (g *Gateway) runReconnectingChannel(parent context.Context, name string, ch channel.Channel) {
	backoff := newReconnectBackoff()

	for parent.Err() == nil {
		recvCtx, recvCancel := context.WithCancel(parent)
		recvDone := make(chan struct{})
		go g.pumpInbound(recvCtx, ch, recvDone)

		onlineDone := make(chan struct{})
		go func() {
			defer close(onlineDone)
			g.waitAndBroadcastOnline(recvCtx, name, ch, backoff)
		}()

		err := ch.Start(parent)

		recvCancel()
		<-recvDone
		<-onlineDone

		if parent.Err() != nil {
			_ = ch.Stop()
			return
		}

		if err != nil {
			slog.Warn("channel stopped; will reconnect", "name", name, "error", err)
			g.hub.Broadcast(&message.Event{
				Type: message.EventChannelError,
				Payload: map[string]string{
					"channel": name,
					"error":   err.Error(),
				},
			})
		} else {
			slog.Warn("channel exited without error; reconnecting", "name", name)
		}

		if serr := ch.Stop(); serr != nil {
			slog.Debug("channel stop", "name", name, "error", serr)
		}

		d := backoff.sleep()
		slog.Info("channel reconnect backoff", "name", name, "sleep", d)
		select {
		case <-parent.Done():
			return
		case <-time.After(d):
		}
	}
}

func (g *Gateway) pumpInbound(ctx context.Context, ch channel.Channel, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch.Receive():
			if !ok {
				return
			}
			g.msgIn.Add(1)
			select {
			case g.hub.Inbound() <- msg:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (g *Gateway) waitAndBroadcastOnline(ctx context.Context, name string, ch channel.Channel, b *reconnectBackoff) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ch.Health().Status == channel.StatusConnected.String() {
				b.reset()
				g.hub.Broadcast(&message.Event{
					Type:    message.EventChannelOnline,
					Payload: map[string]string{"channel": name},
				})
				return
			}
		}
	}
}
