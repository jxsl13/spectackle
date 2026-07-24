---
schema: v0
---

## R-0003 graph node removal for true incremental IndexPaths
kind: research
state: draft
created: 2026-07-24

Backlog design question (blocks real M2 incrementality): memGraph has no delete; IndexPaths is a documented no-op. Options to explore when picked up: (a) per-file node/edge ownership index + RemoveFile(path) then reparse; (b) generation-stamped nodes with lazy sweep; (c) rebuild-from-blob-cache (current IndexAll is already ~40ms syntactic + cached typed pass, so incrementality may simply never pay below ~100k LOC - measure first, M4 perf target is the decision gate).
