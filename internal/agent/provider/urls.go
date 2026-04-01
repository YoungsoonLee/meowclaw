package provider

import "strings"

// Default API path segments (override via config when providers version their URLs).
const (
	DefaultOpenAIChatPath        = "/v1/chat/completions"
	DefaultAnthropicMessagesPath = "/v1/messages"
)

// joinBaseAndPath trims trailing slashes from base and ensures path starts with "/".
func joinBaseAndPath(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
