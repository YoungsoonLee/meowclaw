package gateway

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/YoungsoonLee/meowclaw/internal/config"
)

func (g *Gateway) effectiveMaxAPIBodyBytes() int64 {
	n := g.cfg.Gateway.MaxAPIBodyBytes
	if n <= 0 {
		return config.DefaultMaxAPIBodyBytes
	}
	return n
}

func (g *Gateway) effectiveMaxInputRunes() int {
	return maxInputRunesForAgent(g.cfg.Agent)
}

// gatewayAuthRequired returns whether APIToken is configured.
func (g *Gateway) gatewayAuthRequired() bool {
	return strings.TrimSpace(g.cfg.Gateway.APIToken) != ""
}

// gatewayAuthOK validates Bearer token or, when allowQueryToken, ?token= (for browser WebSocket).
func (g *Gateway) gatewayAuthOK(w http.ResponseWriter, r *http.Request, allowQueryToken bool) bool {
	want := strings.TrimSpace(g.cfg.Gateway.APIToken)
	if want == "" {
		return true
	}
	if allowQueryToken {
		if constantTimeStringEq(r.URL.Query().Get("token"), want) {
			return true
		}
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		got := strings.TrimSpace(auth[len(prefix):])
		if constantTimeStringEq(got, want) {
			return true
		}
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func constantTimeStringEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (g *Gateway) websocketOriginAllowed(r *http.Request) bool {
	if g.cfg.Gateway.AllowAnyWebsocketOrigin {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Many non-browser WebSocket clients omit Origin.
		return true
	}
	for _, allowed := range g.websocketAllowedOrigins() {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (g *Gateway) websocketAllowedOrigins() []string {
	if len(g.cfg.Gateway.TrustedOrigins) > 0 {
		return g.cfg.Gateway.TrustedOrigins
	}
	host := g.cfg.Gateway.Host
	port := g.cfg.Gateway.Port
	if host == "" {
		host = "127.0.0.1"
	}
	// Browsers often use 127.0.0.1 or localhost even when gateway binds to 0.0.0.0.
	return []string{
		fmt.Sprintf("http://%s:%d", host, port),
		fmt.Sprintf("http://127.0.0.1:%d", port),
		fmt.Sprintf("http://localhost:%d", port),
	}
}

func maxInputRunesForAgent(a config.AgentConfig) int {
	if a.MaxInputRunes > 0 {
		return a.MaxInputRunes
	}
	return config.DefaultMaxInputRunes
}

func bindAddressExposesLAN(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "" {
		// Addr like ":6820" listens on all interfaces.
		return true
	}
	switch h {
	case "127.0.0.1", "localhost", "::1", "ip6-localhost", "ip6-loopback":
		return false
	default:
		return true
	}
}
