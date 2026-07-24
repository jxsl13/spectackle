---
schema: v0
---

## intent
- T-0018 graceful shutdown for serve -http (SIGINT/SIGTERM): graceful -http shutdown: SIGTERM -> clean exit 0, deferred deregister runs

## CLI-001 {applies: go:main.main}
WHEN `serve` runs on stdio, the spectacle CLI SHALL emit only JSON-RPC frames on stdout and route every log line to stderr, so a single misplaced print can never corrupt the MCP transport.
