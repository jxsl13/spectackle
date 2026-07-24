---
schema: v0
---

## intent
- T-0018 graceful shutdown for serve -http (SIGINT/SIGTERM): graceful -http shutdown: SIGTERM -> clean exit 0, deferred deregister runs

## CLI-001 {applies: go:main.main}
WHEN `serve` runs on stdio, the spectacle CLI SHALL emit only JSON-RPC frames on stdout and route every log line to stderr, so a single misplaced print can never corrupt the MCP transport.

## CLI-002
The spectackle binary SHALL ship its own MCP client so a headless tool call needs no external wrapper, using the go-sdk `mcp.Client` rather than hand-rolled JSON-RPC framing.

Rationale: Wrapper reimplementations of the handshake have silently dropped the initialize response (hiding the instructions manifest) and emitted frames without the version tag. Both are impossible when the shipped binary performs the handshake.
