# MeowClaw: CGO + SQLite (FTS5). Runtime needs libsqlite3.
FROM golang:1.26-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
	libsqlite3-dev \
	gcc \
	&& rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=docker
RUN CGO_ENABLED=1 GOOS=linux go build -tags sqlite_fts5 \
	-ldflags "-s -w -X main.version=${VERSION}" \
	-o /out/meowclaw ./cmd/meowclaw

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates \
	libsqlite3-0 \
	wget \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/meowclaw /usr/local/bin/meowclaw
COPY web/static /app/web/static
COPY docker/config.docker.yaml /usr/local/share/meowclaw/config.docker.yaml
COPY docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh /usr/local/bin/meowclaw

WORKDIR /app

ENV HOME=/data

EXPOSE 6820

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD wget -qO- http://127.0.0.1:6820/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD []
