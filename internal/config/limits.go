package config

// DefaultMaxInputRunes caps a single inbound user message (channels + HTTP API + WebSocket).
const DefaultMaxInputRunes = 100_000

// DefaultMaxAPIBodyBytes limits JSON body size for gateway HTTP API routes.
const DefaultMaxAPIBodyBytes = 1 << 20 // 1 MiB
