---
schema: v0
---

## P-0072 resident localhost HTTP is the default operating mode: document it and add -pidfile
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: cmd/spectackle/main.go, README.md

Two requirements, one surface. (1) The server should be run as a background process bound to localhost over Streamable HTTP rather than as a per-call stdio process. The transport already exists (T-0010, T-0018: -http plus graceful SIGINT/SIGTERM shutdown); what is missing is that nothing tells an operator or an agent that this is the intended mode. The README's headless recipe still describes spawning a stdio process per call, which re-indexes on every invocation — measured here: a full index of 163 files runs at every stdio start, while the resident process indexes once and answers subsequent calls from a warm cache. That is the whole point of the resident service, and it is currently undiscoverable.

(2) Backgrounding a process the operator cannot cleanly stop is the missing half. Add an optional -pidfile FLAG taking a user-chosen destination path, written after the listener is bound and removed on graceful shutdown, so stopping the service is a kill on a known file rather than a pgrep. Write-after-bind matters: a pidfile that exists before the port is listening invites a stop command that races the startup.

Scope note: -pidfile is deliberately tied to no transport in particular; it is equally useful for a backgrounded stdio server, and gating it on -http would be an arbitrary restriction.

Rejected: making -http the default with stdio opt-in. MCP clients spawn the binary and speak stdio over the child's pipes; flipping the default breaks every registered client configuration for a convenience that a documented recipe delivers just as well.

## T-0102 serve -pidfile plus a README recipe for the resident localhost HTTP service
kind: task
state: active
created: 2026-07-24
parent: P-0072
targets: cmd/spectackle/main.go, cmd/spectackle/main_test.go, README.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Two halves of one operating mode. The Streamable HTTP transport already exists (serve -http ADDR, with graceful SIGINT/SIGTERM shutdown and a 5s drain, see httpShutdownTimeout in main.go), but nothing tells an operator or an agent that a resident background service is the intended way to run this. Measured cost of the alternative: every stdio invocation re-indexes the workspace from scratch — 163 files on this repo at each start — while the resident process indexes once and answers later calls from a warm cache. Second half: a process backgrounded without a stoppable handle is a process the operator has to pgrep for. Add an optional pidfile.

SCOPE (disjoint, lease exactly these three)
  cmd/spectackle/main.go
  cmd/spectackle/main_test.go
  README.md
Do NOT touch internal/resolve or internal/mcpserver (sibling tasks own both). Do NOT touch docs/roadmap.md or docs/design-wasm-parsers.md. .spectackle files are server-owned: never edit them by hand.

CONTRACT ALREADY BINDING THIS FILE
CLI-001: WHEN serve runs on stdio, the CLI SHALL emit only JSON-RPC frames on stdout and route every log line to stderr. Anything you print about the pidfile goes to stderr via the existing log package, never to stdout. This is an anchored rule — breaking it shows up as drift.

PART 1 — -pidfile
Add a `-pidfile PATH` flag to the serve FlagSet, empty by default (feature off). Deliberately NOT gated on -http: a backgrounded stdio server benefits identically, and gating it would be an arbitrary restriction.
Semantics:
  - Write the file only AFTER the listener is bound, not before. A pidfile that exists while the port is still coming up invites a stop command that races startup. For the -http path this means after net.Listen succeeds and before serving begins; runHTTP already separates those steps — if it does not, split it rather than writing early. For the stdio path, write it just before Run.
  - Content: the decimal PID and a trailing newline, mode 0o644.
  - Remove the file on graceful shutdown, in a defer that also runs on the error return paths. Do not remove a pidfile you did not write.
  - If the path already exists, fail the command with a clear stderr message and a non-zero exit rather than overwriting — an existing pidfile usually means a live server, and clobbering it strands that process.
  - Creating the parent directory is NOT in scope; a missing directory is an error.
Update the usage() text and the package doc comment at the top of main.go, both of which list the serve flags.

PART 2 — README
The README has a headless recipe describing a stdio process per call. Add the resident-service recipe as the recommended path: start the server bound to 127.0.0.1 on a chosen port with a pidfile, point a client at it, stop it with a kill reading that pidfile. Keep the stdio recipe — it is still what MCP clients that spawn the binary use. Make clear which is which and why. Do not restructure the README or touch the Quickstart section above it.
Explicitly rejected, do not implement: making -http the default with stdio opt-in. MCP clients spawn the binary and speak stdio over the child's pipes; flipping the default breaks every registered client configuration for something a documented recipe delivers just as well.

TESTS (cmd/spectackle/main_test.go, matching the file's existing style)
  1. -pidfile with -http: start serve on 127.0.0.1:0 or a free high port in a goroutine, wait until the file appears, assert its content parses as the current process PID, signal shutdown, assert the file is gone.
  2. pre-existing pidfile: create the file first, assert serve returns non-zero and that the pre-existing file is still intact (not truncated, not removed).
  3. unwritable path (missing parent directory): assert non-zero exit and no panic.
  4. no -pidfile: assert no file is created anywhere and behavior is unchanged.
If the existing serve() signature makes these untestable without a refactor, extract the pidfile handling into a small helper with its own unit test and keep serve() calling it — do not restructure the rest of serve().

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./cmd/spectackle/... -race
  go test ./...
  go vet ./cmd/spectackle/...
  /home/user/spectackle/bin/spectackle lint
Also exercise it by hand once and paste the real transcript: start with -http and -pidfile, cat the pidfile, kill that PID, confirm the file is gone.

EXIT CRITERION
Four tests green under -race, ./... green, vet clean, lint clean, the manual transcript shows create-then-remove, and stdout carries no pidfile chatter (CLI-001).

ROLLBACK
One flag plus a helper plus tests plus README prose. Reverting is a git checkout of the three files; no schema, stored format, record or anchor changes, and the flag defaults to off so an un-reverted build behaves exactly as today.

REPORT BACK
The flag's final semantics as implemented, each test's real output, the manual transcript, and anything you deliberately did NOT do.

## P-0073 self-contained Go MCP client: retire the external wrapper scripts for headless tool calls
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: cmd/spectackle/main.go, README.md

Driving this server headlessly today requires a hand-written wrapper in another language: the operator must implement the JSON-RPC framing, the initialize/notifications-initialized handshake, and for the HTTP transport also session-header propagation and SSE-vs-JSON response demultiplexing. That is protocol plumbing the binary should own. Concrete failure modes already paid for during this development cycle: a wrapper that discarded the initialize response silently hid the entire instructions manifest, so the tiered orchestrator workflow was never applied; another sent a bare tool-call frame without the version tag and got refused. Both are handshake bugs that cannot occur when the shipped client performs the handshake.

Cost is near zero because the client side of the protocol is already a dependency: go-sdk v1.6.1 provides mcp.NewClient, ClientSession.CallTool, StreamableClientTransport (Endpoint) and CommandTransport (exec.Cmd). No hand-rolled JSON-RPC, no new module. The repo is CGO_ENABLED=0 pure Go and stays that way.

Shape: a call subcommand that reaches a resident server over HTTP when given an endpoint, and otherwise spawns the server itself over stdio, then issues one or more tool calls and prints the dense text content the tools already emit. Output must stay byte-identical to what a wrapper prints today, because the tool grammar in docs/tools.md is what agents parse.

Split into two tasks against a live scope conflict: the client logic lands first in its own internal package, which nothing else is touching, while cmd/spectackle/main.go and README.md are held by the in-flight -pidfile task. Wiring the subcommand and rewriting the README recipe follows once that lands. The package split is not merely scheduling: it keeps cmd/spectackle a thin dispatch shell, matching how the rest of this repo puts logic under internal/, and makes transport selection and result rendering unit-testable without spawning a process.

Rejected: keeping the wrapper and documenting it better. The handshake bugs above were not documentation failures; they were reimplementations of protocol the binary already links.
