APPLICATION_NAME := mcp-bridge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X github.com/ankit-lilly/mcp-bridge/internal/version.Version=$(VERSION) \
	-X github.com/ankit-lilly/mcp-bridge/internal/version.Commit=$(COMMIT) \
	-X github.com/ankit-lilly/mcp-bridge/internal/version.BuildDate=$(DATE)

GCFLAGS := all=-l
BUILDFLAGS := -trimpath -gcflags="$(GCFLAGS)"

.PHONY: all build test test-race vet clean bench fmt

all: build

build:
	CGO_ENABLED=0 go build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" -o bin/mcp-bridge ./cmd/bridge

fmt:
	gofmt -s -w .

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

bench:
	go test ./internal/bridge -bench . -benchmem
	go test ./internal/remote -bench . -benchmem

lint:
	go vet ./...
	go test ./... -count=1
