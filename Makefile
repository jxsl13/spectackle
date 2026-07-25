GO         ?= go
BIN        := bin/spectackle
GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@latest

.PHONY: all build vet test cover fuzz lint-specs smoke clean release-snapshot dev dev-stop dev-status

# Seconds of fuzzing per target in `make fuzz` (M4 linter hardening).
FUZZTIME ?= 10s

# Minimum total statement coverage (percent) enforced by `make cover`.
# Baseline at introduction: 75.0%; kept below to absorb noise, ratchet upward.
COVER_MIN ?= 70

# `make dev` target: address/pidfile/log for the resident dev server it
# manages, and the bounds on the readiness retry loop that proves the server
# actually answers before the target returns. Overridable like GO/BIN/etc
# above. Default port deliberately differs from any hand-started resident
# server (e.g. an orchestration server on 127.0.0.1:7411) so `make dev`
# never contends with one.
DEV_ADDR        ?= 127.0.0.1:7412
DEV_PIDFILE     ?= bin/dev.pid
DEV_LOG         ?= bin/dev.log
DEV_READY_TRIES ?= 30
DEV_READY_SLEEP ?= 0.5

all: build vet test lint-specs smoke cover

build:
	CGO_ENABLED=0 $(GO) build -o $(BIN) ./cmd/spectackle

# dev rebuilds the binary and (re)starts it as a resident Streamable HTTP
# server, leaving exactly one instance running at DEV_ADDR with a stoppable
# pidfile at DEV_PIDFILE. Idempotent: running it twice in a row stops the
# server it started the first time before starting the replacement, so
# there is never a second process bound to the port. Readiness is proven
# with a real `spectackle call` against the running server (a bound socket
# answers before the index is warm, so a port probe would be a false
# positive) in a loop bounded by DEV_READY_TRIES/DEV_READY_SLEEP; if the
# server process dies (e.g. the port is already taken by something else)
# or never answers in time, the target fails loudly instead of hanging.
dev: build dev-stop
	@mkdir -p $(dir $(DEV_PIDFILE))
	@rm -f $(DEV_LOG)
	@./$(BIN) serve -root . -http $(DEV_ADDR) -pidfile $(DEV_PIDFILE) >$(DEV_LOG) 2>&1 & \
	pid=$$!; \
	i=0; \
	while [ $$i -lt $(DEV_READY_TRIES) ]; do \
	  if ! kill -0 $$pid 2>/dev/null; then \
	    echo "dev: server process exited before becoming ready on $(DEV_ADDR); log:" >&2; \
	    cat $(DEV_LOG) >&2; \
	    exit 1; \
	  fi; \
	  if ./$(BIN) call -http $(DEV_ADDR) state >/dev/null 2>&1; then \
	    echo "dev: resident server ready on $(DEV_ADDR) (pid $$pid, pidfile $(DEV_PIDFILE))"; \
	    exit 0; \
	  fi; \
	  i=$$((i + 1)); \
	  sleep $(DEV_READY_SLEEP); \
	done; \
	echo "dev: server on $(DEV_ADDR) did not answer a real call after $(DEV_READY_TRIES) tries; log:" >&2; \
	cat $(DEV_LOG) >&2; \
	kill $$pid 2>/dev/null; \
	exit 1

# dev-stop stops the resident dev server tracked by DEV_PIDFILE, if any.
# Semantics match cmd/spectackle/main.go's pidfile handling: a live PID is
# stopped (SIGTERM, falling back to SIGKILL if it won't exit) and its
# pidfile removed; a stale pidfile (recorded PID no longer running -- serve
# itself removes the file on clean shutdown, so a leftover file always
# means a dead process) is cleared rather than left to wedge the next
# `serve -pidfile`, which refuses to start while the file exists. Doing
# nothing when no pidfile exists is success, not failure, so a fresh clone
# or a repeated stop is a no-op.
dev-stop:
	@if [ -f $(DEV_PIDFILE) ]; then \
	  pid=$$(cat $(DEV_PIDFILE) 2>/dev/null); \
	  if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
	    echo "dev-stop: stopping resident server (pid $$pid)..."; \
	    kill "$$pid" 2>/dev/null; \
	    i=0; \
	    while kill -0 "$$pid" 2>/dev/null && [ $$i -lt $(DEV_READY_TRIES) ]; do \
	      i=$$((i + 1)); \
	      sleep $(DEV_READY_SLEEP); \
	    done; \
	    if kill -0 "$$pid" 2>/dev/null; then \
	      echo "dev-stop: pid $$pid did not exit in time; sending SIGKILL" >&2; \
	      kill -9 "$$pid" 2>/dev/null || true; \
	    fi; \
	    rm -f $(DEV_PIDFILE); \
	    echo "dev-stop: stopped"; \
	  else \
	    echo "dev-stop: stale pidfile $(DEV_PIDFILE) (pid $$pid not running); clearing"; \
	    rm -f $(DEV_PIDFILE); \
	  fi; \
	else \
	  echo "dev-stop: not running"; \
	fi

# dev-status reports whether the DEV_PIDFILE-tracked resident dev server is
# running, without changing anything. Exits non-zero when not running (a
# stale pidfile counts as not running), so it composes in scripts.
dev-status:
	@if [ -f $(DEV_PIDFILE) ]; then \
	  pid=$$(cat $(DEV_PIDFILE) 2>/dev/null); \
	  if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
	    echo "dev-status: running (pid $$pid, $(DEV_ADDR), pidfile $(DEV_PIDFILE))"; \
	  else \
	    echo "dev-status: not running (stale pidfile $(DEV_PIDFILE), pid $$pid gone)"; \
	    exit 1; \
	  fi; \
	else \
	  echo "dev-status: not running"; \
	  exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

fuzz:
	$(GO) test ./internal/ears/ -fuzz=FuzzLintSentence -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/ears/ -fuzz=FuzzParseRules -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/ears/ -fuzz=FuzzStripFrontMatter -fuzztime=$(FUZZTIME)

# bench meters the MCP text surface: a scripted all-state lifecycle over a
# generated fixture, bytes and estimated tokens per tool, validity-gated.
# BENCH_AGAINST=path/to/other/binary A/Bs two builds.
bench: build
	@if [ -n "$(BENCH_AGAINST)" ]; then ./$(BIN) bench -against "$(BENCH_AGAINST)"; else ./$(BIN) bench; fi

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

# Build all release targets locally without publishing, to sanity-check the
# goreleaser config (.goreleaser.yaml) before tagging. Uses `go run` so a
# goreleaser binary need not be pre-installed; override GORELEASER=goreleaser
# to use a local install instead.
release-snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

clean:
	rm -rf bin
