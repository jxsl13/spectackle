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
- P-0010 output diet: elide root-rule text in packs, no sw replay for fresh agents, edge dedup, drop empty sections: output diet: r-root eliding, no sw replay, edge dedup, empty sections omitted (~77% smaller packs)
- P-0014 MCP prompts: /spectacle workflow entry points (workflow + next): workflow+next MCP prompts live: slash-command entry with live state, implementer protocol briefing
- P-0017 state tool + /spectacle-state prompt: one structured, read-only full picture: state tool + prompt live: sectioned read-only full picture, SPX-MCP-005 anchored

## SPX-MCP-004 {applies: go:mcpserver.Server.draft}
WHEN a draft context pack is rendered, the server SHALL emit root-scoped rules as one r-root ID record and omit empty pack sections entirely.

Rationale: root rules are stable knowledge after the first get; empty sections are pure filler.
## SPX-MCP-005 {applies: go:mcpserver.Server.state}
WHEN the state tool is invoked, the server SHALL render the `#version` `#items` `#rules` `#graph` `#swarm` `#drift` `#health` sections as dense records and perform zero `.spectacle` writes.

## SPX-MCP-006 {applies: go:mcpserver.Server.research}
WHEN the research tool is invoked, the server SHALL render the condensed pack sections `#impact` `#contracts` `#rejections` `#history` `#docs` `#gaps` `#open` and perform zero `.spectacle` writes.

## SPX-MCP-007 {applies: go:mcpserver.Server.grill}
WHEN the grill tool completes its critique pack, the server SHALL stamp the item header field `grilled:` with the current date as fold-proof evidence.

## MCP-001 {applies: go:mcpserver.Server.decideAsk}
WHEN `decide op=ask` stores a decision's option set, the spectacle server SHALL persist each option as its own `option:` body line so `decideOptions` reproduces every option byte-identically.

Rationale: comma-joined storage shattered comma-containing options — no byte-identical answer could ever match (found live on D-0002)

## MCP-002 {applies: go:graph.Node}
WHEN a node record is rendered and `EndLine` is known, the record renderer SHALL emit the definition span as `file:start-end` so agents see where a function ends without reading the file.
