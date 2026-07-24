GO         ?= go
BIN        := bin/spectackle
GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@latest

.PHONY: all build vet test lint-specs smoke clean release-snapshot

all: build vet test lint-specs smoke

build:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/spectackle

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

lint-specs: build
	./$(BIN) lint .

# Pipe an MCP handshake + tools/list into the stdio server and check that the
# tool surface is present (SPX-ARC-001: stdout must be pure JSON-RPC).
smoke: build
	@(printf '%s\n%s\n%s\n' \
	  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' \
	  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
	  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; sleep 1) \
	  | ./$(BIN) serve -root . 2>/dev/null \
	  | grep -q '"draft"' && echo "smoke: OK (tools listed)" || (echo "smoke: FAILED"; exit 1)

# Build all release targets locally without publishing, to sanity-check the
# goreleaser config (.goreleaser.yaml) before tagging. Uses `go run` so a
# goreleaser binary need not be pre-installed; override GORELEASER=goreleaser
# to use a local install instead.
release-snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

clean:
	rm -rf bin
