---
schema: v0
---

## P-0010 output diet: elide root-rule text in packs, no sw replay for fresh agents, edge dedup, drop empty sections
kind: proposal
state: active
created: 2026-07-24
targets: internal/mcpserver/tools.go, go:coord.Open, go:graph.memGraph.Impact

Four measured verbosity sinks: (1) draft context packs repeat ALL root rules verbatim (~450 tok/call) - emit root-scoped rules as one ID-only line, full text only for dir-scoped rules; (2) fresh agent registrations replay old sw events (cursor starts at 0) - initialize cursor to MAX(seq) on first registration, history stays available via swarm; (3) Impact emits duplicate edges under Both traversal - dedup; (4) empty #impact/#contracts/#rejections sections still print headers + filler - omit empty sections entirely.

## T-0015 implement output diet (packs, sw cursor, edge dedup, empty sections)
kind: task
state: done
created: 2026-07-24
parent: P-0010

Scope ONLY: internal/mcpserver/tools.go + tools_test.go, internal/coord/coord.go + coord_test.go, internal/graph/impact.go + graph_test.go, docs/tools.md (leased by agent-fsm right now: retry claim at the end, else report). (1) draft pack #contracts: rules from root scope (ScopeDir '.') -> single line 'r-root <ID> <ID> ...'; deeper rules keep full r lines. (2) coord.Open: on FIRST insert of an agent row set cursor=MAX(seq) (upsert keeps existing cursor). (3) memGraph.Impact: dedup edges. (4) draft pack: omit empty sections (no header, no filler line). Tests for all four. go test ./internal/... -race for the three packages green.
