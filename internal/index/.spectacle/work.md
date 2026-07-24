---
schema: v0
---

## T-0026 honor config.yaml ignore globs in IndexAll
kind: task
state: done
created: 2026-07-24

Scope ONLY: internal/index/indexer.go + indexer_test.go (extend existing test file or the cache test file - your choice, one place). docs/architecture.md claims the walk honors config ignore globs; reality: fixed ignoreDirs only, MatchIgnore is exported but unused. Fix: index.New gains variadic tail New(g, s, parsers, resolvers, ignore ...string) storing the globs (backward compatible - all existing callers compile unchanged); IndexAll walk: after the ignoreDirs check, skip any file whose repo-relative path matches MatchIgnore(ix.ignore, rel); for directories, skip when MatchIgnore matches rel+"/" pattern semantics is messy - keep it simple: check files only (dirs still pruned via ignoreDirs), document that dir-level pruning of custom globs is an optimization for later. Also compute the typespass module key... NO - typespass is out of scope, note the asymmetry in the report instead. Test: temp tree with gen/x.go + config-style ignore ["gen/**"] -> node absent; without ignore -> present. NO server.go changes (orchestrator passes ws.Cfg.Ignore).

## R-0003 graph node removal for true incremental IndexPaths
kind: research
state: draft
created: 2026-07-24

Backlog design question (blocks real M2 incrementality): memGraph has no delete; IndexPaths is a documented no-op. Options to explore when picked up: (a) per-file node/edge ownership index + RemoveFile(path) then reparse; (b) generation-stamped nodes with lazy sweep; (c) rebuild-from-blob-cache (current IndexAll is already ~40ms syntactic + cached typed pass, so incrementality may simply never pay below ~100k LOC - measure first, M4 perf target is the decision gate).
