---
schema: v0
---

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

## P-0075 wire the call subcommand: headless tool calls with no external wrapper
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: cmd/spectackle/main.go, README.md

Completes the self-contained-client line. internal/mcpclient now provides Dial/Call/Instructions/Close over both transports with rendering proven byte-identical between them, but nothing in the binary exposes it, so driving the server headlessly still requires an external script. cmd/spectackle was held by the pidfile task while the package landed; it is free now.

Shape: call reaches a resident server when given an endpoint and otherwise spawns one over stdio, reads one JSON tool call per stdin line or takes a single call from arguments, and writes the tool's dense text to stdout. Output must pass through byte-identically because the grammar in docs/tools.md is what agents parse, and a per-call prefix or JSON envelope would break every consumer.

CLI-001 constrains this file: serve on stdio may put only JSON-RPC frames on stdout. call is a client, not that server, so its stdout is the tool text — but its own diagnostics still belong on stderr, and a spawned child server's stderr must not be interleaved into the rendered output.

Exit status has to carry the refusal signal: the client already returns text plus a non-nil error when a result is flagged IsError, and the subcommand must turn that into a non-zero exit while still printing the refusal text, so a shell caller can branch on a gate refusal without parsing prose.

The README's headless recipe is then rewritten against this subcommand, which removes the last reason anyone needs to reimplement the handshake.

## T-0105 call subcommand over internal/mcpclient, README headless recipe rewritten
kind: task
state: active
created: 2026-07-24
parent: P-0075
targets: cmd/spectackle/main.go, cmd/spectackle/main_test.go, README.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Expose internal/mcpclient through the binary so a headless tool call needs no external script. The package already provides Dial/Call/Instructions/Close over HTTP and stdio with rendering proven byte-identical between transports; nothing in the CLI reaches it yet.

SCOPE (disjoint, lease exactly these three)
  cmd/spectackle/main.go
  cmd/spectackle/main_test.go
  README.md
Do NOT touch internal/mcpclient (finished, treat its API as fixed), internal/mcpserver (a sibling task owns it right now), internal/resolve (another sibling owns it), or docs/. .spectackle files are server-owned: never edit them by hand.

API YOU CONSUME (do not change it)
  mcpclient.Config{Endpoint, Command, Root string}
  mcpclient.Call{Name string; Arguments map[string]any}
  mcpclient.Dial(ctx, cfg) (*Session, error)
  (*Session).Call(ctx, Call) (string, error)   // text AND non-nil error when the result is flagged IsError
  (*Session).Instructions() string
  (*Session).Close() error

SUBCOMMAND
  spectackle call [-root DIR] [-http ADDR] [-instructions] [NAME [JSON]]
  -http ADDR   reach a resident server at that address; absent = spawn one over stdio
  -root DIR    workspace root, same default as serve
  -instructions  print the server's instructions manifest and exit 0 (this is the field a hand-written wrapper is most likely to drop, which is why it gets a flag)
Input modes: with NAME and optional JSON arguments, issue exactly that one call. With no positional arguments, read one JSON object per non-empty stdin line, each of the shape {"name": ..., "arguments": {...}}, and issue them in order over ONE session — reconnecting per line would re-index on every call, which is the cost this whole line of work exists to remove.
Output: the tool's text to stdout, byte-identical, one trailing newline per call, no prefix, no JSON envelope, no separator banner. The dense grammar in docs/tools.md is what agents parse. Everything else — connection diagnostics, spawned-server stderr — goes to stderr.
Exit status: 0 when every call succeeded; non-zero when any call returned the IsError signal, AFTER printing that call's refusal text to stdout. A shell caller must be able to branch on a gate refusal without parsing prose. In multi-call stdin mode, keep going after a refusal and exit non-zero at the end; report which line failed on stderr.
Register the subcommand in run()'s switch and document it in both usage() and the package doc comment, both of which list the subcommands.

CLI-001 NOTE
That contract binds serve on stdio, not call: call is a client, so its stdout legitimately carries tool text. Do not weaken or reword the rule; just do not let a spawned child server's stderr leak into stdout.

TESTS (main_test.go, matching the file's existing style)
  1. single call, stdio: call state against a temp workspace; assert stdout starts with the #version section and stderr carries no tool text.
  2. single call, http: start a server on a probed free port, call state with -http; assert stdout is byte-identical to case 1 for the same call. Pin SPECTACKLE_AGENT via t.Setenv so both runs render the same agent name — internal/mcpclient's own test does this for the same reason.
  3. multi-call stdin: two lines, one session; assert both outputs appear in order.
  4. refusal: call find with {} (its q argument is required); assert non-zero exit AND that the refusal text reached stdout.
  5. -instructions: assert non-empty output containing a manifest marker, exit 0.
Probe a free port by listening on :0 and reading the address back; never hardcode one — the suite runs alongside other work.

README
Rewrite the headless recipe against this subcommand: a one-shot call, a multi-call stdin batch, and the resident variant using -http together with the pidfile recipe added earlier. Delete the wrapper-script instructions it replaces — leaving both would invite the reimplementation this work exists to prevent. Do not restructure the README or touch the Quickstart section.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./cmd/spectackle/... -race
  go test ./...
  go vet ./cmd/spectackle/...
  /home/user/spectackle/bin/spectackle lint
Also paste one real transcript of the built binary issuing a state call over each transport, showing identical bytes.

EXIT CRITERION
Five tests green under -race, both transports byte-identical, refusal exits non-zero with its text on stdout, ./... green, vet clean, lint clean, and the README no longer instructs anyone to write a wrapper.

ROLLBACK
One subcommand, its tests, and README prose. git checkout of the three files restores the prior state; internal/mcpclient stays, unreferenced, exactly as it is today. No schema, stored format, record or anchor change.

REPORT BACK
The subcommand's final flags and semantics, each test's real output, the two transcripts with their byte comparison, and anything you deliberately did NOT do.
