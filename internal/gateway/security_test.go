package gateway

import (
	"testing"

	"github.com/YoungsoonLee/meowclaw/internal/config"
)

func TestBindAddressExposesLAN(t *testing.T) {
	if !bindAddressExposesLAN("") {
		t.Error("empty host should be treated as exposing all interfaces")
	}
	if bindAddressExposesLAN("127.0.0.1") {
		t.Error("127.0.0.1 should not expose LAN")
	}
	if !bindAddressExposesLAN("0.0.0.0") {
		t.Error("0.0.0.0 should be treated as non-loopback (all interfaces)")
	}
}

func TestMaxInputRunesForAgent(t *testing.T) {
	if maxInputRunesForAgent(config.AgentConfig{}) != config.DefaultMaxInputRunes {
		t.Errorf("default runes mismatch")
	}
	if maxInputRunesForAgent(config.AgentConfig{MaxInputRunes: 42}) != 42 {
		t.Errorf("override runes = %d, want 42", maxInputRunesForAgent(config.AgentConfig{MaxInputRunes: 42}))
	}
}
