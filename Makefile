GO         ?= go
BIN        := bin/spectackle
GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@latest

.PHONY: all build vet test cover fuzz lint-specs smoke clean release-snapshot dev dev-stop dev-status

# Seconds of fuzzing per target in `make fuzz` (M4 linter hardening).
FUZZTIME ?= 10s

# Minimum total statement coverage (percent) enforced by `make cover`.
# Baseline at introduction: 75.0%; kept below to absorb noise, ratchet upward.
COVER_MIN ?= 70

# `make dev` resident server: address, pidfile and log location, and how
# long (seconds) to wait for a real answer / a stop before giving up.
DEV_ADDR    ?= 127.0.0.1:7331
DEV_PIDFILE ?= bin/spectackle-dev.pid
DEV_LOG     ?= bin/spectackle-dev.log
DEV_TIMEOUT ?= 30

all: build vet test lint-specs smoke cover

build:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/spectackle

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

fuzz:
	$(GO) test ./internal/ears/ -fuzz=FuzzLintSentence -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/ears/ -fuzz=FuzzParseRules -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/ears/ -fuzz=FuzzStripFrontMatter -fuzztime=$(FUZZTIME)

cover:
	@mkdir -p bin
	$(GO) test -coverprofile=bin/cover.out ./...
	@$(GO) tool cover -func=bin/cover.out | awk -v min=$(COVER_MIN) '\
	  /^total:/ { sub(/%/, "", $$3); \
	    if ($$3 + 0 < min) { printf "coverage %s%% below COVER_MIN %s%%\n", $$3, min; exit 1 } \
	    else { printf "coverage %s%% (min %s%%)\n", $$3, min } }'

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

# Rebuild and (re)start the resident server for this workspace: this repo
# develops itself with itself, so the resident server IS the product under
# change, and CONTRIBUTING.md requires this after every merged feature or
# fix (a stale binary answers plausibly from code that no longer exists).
# `dev-stop` runs first unconditionally so this is idempotent and never
# leaves two servers on $(DEV_ADDR): it stops a live one, clears a stale
# pidfile whose PID is dead (serve's -pidfile is O_EXCL and refuses to start
# over an existing file), or no-ops if nothing was running. Readiness is
# proven with a real `call state` round trip in a bounded loop, not a socket
# probe — the port is bound before indexing finishes, so a socket accepts
# connections well before the server can answer anything.
dev: build dev-stop
	@mkdir -p $(dir $(DEV_PIDFILE))
	@rm -f $(DEV_LOG)
	@nohup ./$(BIN) serve -root . -http $(DEV_ADDR) -pidfile $(DEV_PIDFILE) \
	  >$(DEV_LOG) 2>&1 & \
	server_pid=$$!; \
	echo "dev: starting spectackle (pid $$server_pid) on $(DEV_ADDR)"; \
	i=0; \
	while [ $$i -lt $(DEV_TIMEOUT) ]; do \
	  if grep -q '^serve: ' $(DEV_LOG) 2>/dev/null; then \
	    echo "dev: FAILED - server reported an error before becoming ready; log:"; \
	    cat $(DEV_LOG); \
	    exit 1; \
	  fi; \
	  if [ -f $(DEV_PIDFILE) ] && ./$(BIN) call -http $(DEV_ADDR) state >/dev/null 2>>$(DEV_LOG); then \
	    echo "dev: ready - pid $$(cat $(DEV_PIDFILE)) answering on $(DEV_ADDR) (pidfile $(DEV_PIDFILE))"; \
	    exit 0; \
	  fi; \
	  i=$$((i + 1)); \
	  sleep 1; \
	done; \
	echo "dev: FAILED - timed out after $(DEV_TIMEOUT)s waiting for $(DEV_ADDR) to answer a real \`call state\`; log:"; \
	cat $(DEV_LOG); \
	exit 1

# Stop the resident dev server started by `make dev`, if any. Idempotent:
# succeeds with no error whether a server is running, a stale pidfile is
# left over (PID no longer alive - it's removed so serve's O_EXCL check
# doesn't wedge the next `make dev`), or nothing has ever been started
# (fresh clone). Never leaves the pidfile behind on any exit path.
dev-stop:
	@if [ ! -f $(DEV_PIDFILE) ]; then \
	  echo "dev-stop: not running (no pidfile at $(DEV_PIDFILE))"; \
	  exit 0; \
	fi; \
	pid=$$(cat $(DEV_PIDFILE) 2>/dev/null); \
	if [ -z "$$pid" ] || ! kill -0 $$pid 2>/dev/null; then \
	  echo "dev-stop: stale pidfile $(DEV_PIDFILE) (pid $$pid not running); clearing it"; \
	  rm -f $(DEV_PIDFILE); \
	  exit 0; \
	fi; \
	echo "dev-stop: stopping pid $$pid"; \
	kill $$pid 2>/dev/null || true; \
	i=0; \
	while [ -f $(DEV_PIDFILE) ] && [ $$i -lt $(DEV_TIMEOUT) ]; do \
	  sleep 1; \
	  i=$$((i + 1)); \
	done; \
	if [ -f $(DEV_PIDFILE) ]; then \
	  echo "dev-stop: pid $$pid did not shut down within $(DEV_TIMEOUT)s; forcing"; \
	  kill -9 $$pid 2>/dev/null || true; \
	  rm -f $(DEV_PIDFILE); \
	fi; \
	echo "dev-stop: stopped"

# Report whether a `make dev` server is currently running for this
# workspace, without starting or stopping anything.
dev-status:
	@if [ -f $(DEV_PIDFILE) ] && kill -0 $$(cat $(DEV_PIDFILE)) 2>/dev/null; then \
	  echo "dev-status: running - pid $$(cat $(DEV_PIDFILE)) on $(DEV_ADDR) (pidfile $(DEV_PIDFILE))"; \
	else \
	  echo "dev-status: not running"; \
	fi

# Build all release targets locally without publishing, to sanity-check the
# goreleaser config (.goreleaser.yaml) before tagging. Uses `go run` so a
# goreleaser binary need not be pre-installed; override GORELEASER=goreleaser
# to use a local install instead.
release-snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

clean:
	rm -rf bin
