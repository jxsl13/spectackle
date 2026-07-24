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
