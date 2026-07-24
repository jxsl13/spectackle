GO      ?= go
BIN     := bin/spectacle

.PHONY: all build vet test lint-specs smoke clean

all: build vet test lint-specs smoke

build:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/spectacle

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

clean:
	rm -rf bin
