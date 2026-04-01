package provider

import "testing"

func TestJoinBaseAndPath(t *testing.T) {
	tests := []struct {
		base, path, want string
	}{
		{"https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://proxy.example", "v1/chat/completions", "https://proxy.example/v1/chat/completions"},
		{"https://h.example", "", "https://h.example"},
	}
	for _, tt := range tests {
		if got := joinBaseAndPath(tt.base, tt.path); got != tt.want {
			t.Errorf("joinBaseAndPath(%q, %q) = %q, want %q", tt.base, tt.path, got, tt.want)
		}
	}
}
