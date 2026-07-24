---
schema: v0
---

## T-0103 internal/mcpclient: transport selection, tool calls, dense result rendering
kind: task
state: active
created: 2026-07-24
parent: P-0073
targets: internal/mcpclient/client.go, internal/mcpclient/client_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Give the binary its own MCP client so a headless tool call needs no external wrapper script. This task delivers the reusable package only. A sibling task wires the `call` subcommand and the README recipe later — cmd/spectackle/main.go and README.md are held by another in-flight task RIGHT NOW and are out of scope here.

SCOPE (disjoint, lease exactly these two; the package is NEW)
  internal/mcpclient/client.go
  internal/mcpclient/client_test.go
Do NOT touch cmd/spectackle (held), README.md (held), internal/mcpserver (held), internal/resolve (held), docs/. Do NOT add a go.mod dependency: everything needed is already in go-sdk v1.6.1. .spectackle files are server-owned: never edit them by hand.

CONTRACT TO SATISFY
CLI-002 (added under this proposal; read it with get id=CLI-002): the binary SHALL ship its own MCP client so a headless tool call needs no external wrapper, using the go-sdk mcp.Client rather than hand-rolled JSON-RPC framing.

API (keep it this small; this is a package with one job)
  type Config struct {
      Endpoint string  // http(s) URL of a resident server; empty = spawn stdio
      Command  string  // executable to spawn when Endpoint is empty (default: os.Executable())
      Root     string  // workspace root passed to the spawned server
  }
  type Call struct { Name string; Arguments map[string]any }
  func Dial(ctx context.Context, cfg Config) (*Session, error)
  func (s *Session) Call(ctx context.Context, c Call) (string, error)
  func (s *Session) Instructions() string
  func (s *Session) Close() error
Use the SDK, do NOT hand-roll JSON-RPC:
  - mcp.NewClient(&mcp.Implementation{Name: "spectackle", Version: mcpserver.Version}, nil)
  - transport: &mcp.StreamableClientTransport{Endpoint: cfg.Endpoint} when Endpoint is set, else &mcp.CommandTransport{Command: exec.CommandContext(ctx, bin, "serve", "-root", cfg.Root)}
  - client.Connect(ctx, transport, nil) returns a *mcp.ClientSession; it performs initialize and the initialized notification for you — that is the entire point of this task
  - session.CallTool(ctx, &mcp.CallToolParams{Name: ..., Arguments: ...})
Instructions() returns the server's instructions string from the initialize result (see the ClientSession's InitializeResult accessor). Capturing it is not decoration: a wrapper that dropped this field silently hid the whole instructions manifest during this project's own development, which is why the contract exists.

RESULT RENDERING
Call returns the concatenation of the result's text content blocks, joined by newline, with no added framing, prefixes or JSON. The tool output grammar in docs/tools.md is what agents parse, so it must pass through byte-identically. Non-text content blocks are skipped. When the result carries IsError, return the text AND a non-nil error so a caller can distinguish a refusal (`! GATE ...`, `! REJECTED ...`) from success without parsing prose.

ERRORS
Wrap transport and protocol failures with enough context to name the transport and endpoint. A spawn failure must not look like a tool failure. Respect ctx cancellation on every call.

TESTS (internal/mcpclient/client_test.go)
  1. stdio round trip: Dial with an empty Endpoint against a server spawned from the built binary, call `state`, assert the output starts with the `#version` section and contains an `ok spectackle` line. Build the binary into t.TempDir() with `go build` from the test, or skip via t.Skip with a clear reason if the toolchain is unavailable — do NOT depend on a prebuilt /home/user path.
  2. http round trip: start a server on 127.0.0.1 with a free port, Dial with that Endpoint, call `state`, assert the same shape. Assert the SAME rendered bytes as the stdio case for an identical call — that equality IS the test, since transport must not change output.
  3. Instructions(): assert it is non-empty and contains the RECORDS marker the manifest carries.
  4. IsError path: call a tool with arguments that the server refuses (e.g. `find` with no `q`) and assert Call returns both non-empty text and a non-nil error.
  5. Close is idempotent and releases the spawned process.
Use a free port by listening on :0 and reading back the address rather than hardcoding one — the suite runs in parallel with other work.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpclient/... -race -v
  go test ./...
  go vet ./internal/mcpclient/...
  /home/user/spectackle/bin/spectackle lint

EXIT CRITERION
Five tests green under -race, both transports producing byte-identical rendered output for the same call, ./... green, vet clean, lint clean, and no new module requirement in go.mod (confirm with `git diff go.mod go.sum` showing no change).

ROLLBACK
One new package, imported by nothing until the sibling wiring task lands. Deleting the directory restores the prior state exactly; no schema, stored format, record, anchor or dependency changes.

REPORT BACK
The final API as implemented, each test's real output, the byte-equality evidence for the two transports, `git diff go.mod go.sum` output, and anything you deliberately did NOT do.
