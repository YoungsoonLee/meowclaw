package gateway

import "time"

// Defaults for channel auto-reconnect (exponential backoff).
const (
	reconnectBackoffInitial = time.Second
	reconnectBackoffMax     = 5 * time.Minute
)

type reconnectBackoff struct {
	nextDelay time.Duration
	max       time.Duration
}

func newReconnectBackoff() *reconnectBackoff {
	return &reconnectBackoff{
		nextDelay: reconnectBackoffInitial,
		max:       reconnectBackoffMax,
	}
}

func (b *reconnectBackoff) reset() {
	b.nextDelay = reconnectBackoffInitial
}

// sleep returns the delay just applied; caps at max and doubles for the next call.
func (b *reconnectBackoff) sleep() time.Duration {
	d := b.nextDelay
	if d > b.max {
		d = b.max
	}
	if b.nextDelay < b.max {
		b.nextDelay *= 2
		if b.nextDelay > b.max {
			b.nextDelay = b.max
		}
	}
	return d
}
