---
schema: v0
prefix: SPX-MCP
---

# MCP surface contracts

## SPX-MCP-001
WHILE serving a tool call, the server SHALL keep cumulative file reads under 1 MiB and respond within 2 seconds on a warm cache.

## SPX-MCP-002
The server SHALL render tool results in the dense line grammar of docs/tools.md instead of JSON objects.

Rationale: the line grammar is the primary token-efficiency lever (~5x smaller).

## SPX-MCP-003
WHEN a tool call arrives, the server SHALL refresh the cache from the versioned .spectacle files before answering so that disk state is never stale by more than 300 milliseconds.

Rationale: the files are the source of truth; the cache only accelerates them.

## intent
- P-0003 code search matches file paths and signatures; drop stale M1 placeholder texts: find scope=code matches file paths and signatures (SPX-GRA-003); stale M1 placeholder texts removed
- P-0004 swarm registry hygiene: sweep removes stale agents, close deregisters, lease guidance: sweep deletes stale agent rows (SPX-SWM-006), close deregisters, heartbeat re-registers, lease release guidance in manifest+description

## SPX-MCP-004 {applies: go:mcpserver.Server.draft}
WHEN a draft context pack is rendered, the server SHALL emit root-scoped rules as one r-root ID record and omit empty pack sections entirely.

Rationale: root rules are stable knowledge after the first get; empty sections are pure filler.
