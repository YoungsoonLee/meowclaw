VERSION ?= $(shell date +%Y.%-m.%-d)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
TAGS := -tags "sqlite_fts5"

.PHONY: build run clean test

build:
	CGO_ENABLED=1 go build $(TAGS) $(LDFLAGS) -o meowclaw ./cmd/meowclaw

run: build
	./meowclaw up -v

clean:
	rm -f meowclaw

test:
	go test ./...

install: build
	cp meowclaw /usr/local/bin/meowclaw
