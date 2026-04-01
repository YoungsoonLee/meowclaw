package gateway

import (
	"testing"
)

func TestReconnectBackoffDoubles(t *testing.T) {
	b := newReconnectBackoff()
	if d := b.sleep(); d != reconnectBackoffInitial {
		t.Fatalf("first sleep = %v, want %v", d, reconnectBackoffInitial)
	}
	if d := b.sleep(); d != 2*reconnectBackoffInitial {
		t.Fatalf("second sleep = %v, want %v", d, 2*reconnectBackoffInitial)
	}
}

func TestReconnectBackoffReset(t *testing.T) {
	b := newReconnectBackoff()
	_ = b.sleep()
	_ = b.sleep()
	b.reset()
	if d := b.sleep(); d != reconnectBackoffInitial {
		t.Fatalf("after reset first sleep = %v, want %v", d, reconnectBackoffInitial)
	}
}

func TestReconnectBackoffCapsAtMax(t *testing.T) {
	b := &reconnectBackoff{nextDelay: reconnectBackoffMax, max: reconnectBackoffMax}
	if d := b.sleep(); d != reconnectBackoffMax {
		t.Fatalf("sleep = %v, want cap %v", d, reconnectBackoffMax)
	}
	if b.nextDelay != reconnectBackoffMax {
		t.Fatalf("nextDelay = %v, want %v", b.nextDelay, reconnectBackoffMax)
	}
}
