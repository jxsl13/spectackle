---
schema: v0
---

## P-0001 M1 structural core: go/parser indexer + cgo edges
kind: proposal
state: draft
created: 2026-07-24
targets: internal/index/indexer.go, internal/graph/graph.go, internal/resolve/cgo.go

Implement the Go language parser on stdlib go/parser (incl. EndLine spans), populate the in-memory graph, wire the cgo resolver, and make find scope=code / get depth real. Exit: saxpy transcript #impact reproduces; anchors leave pending state.

## P-0002 dedicated unit tests with end-to-end coverage for every implementation
kind: proposal
state: draft
created: 2026-07-24
targets: internal/budget/budget.go, internal/ids/ids.go, internal/graph/graph.go, internal/cache/cache.go, internal/sync/sync.go, internal/item/item.go, internal/journal/journal.go, internal/lifecycle/lifecycle.go, cmd/spectacle/main.go

Every implemented package MUST have a dedicated *_test.go exercising its public surface end to end (not just happy paths): currently untested are internal/budget, internal/ids, internal/graph (Find/Neighbors/Impact), internal/cache (gen-stamp rebuild, ReplaceDocs, Search sanitizing), internal/sync (fast/slow path, MarkDirty), internal/item (parse/upsert/remove round-trip), internal/journal (append/read/rewrite), internal/lifecycle (guards, revocation, archive merge, scopeFor), internal/resolve, internal/store, cmd/spectacle (lint/reindex exit codes). Existing E2E tests (mcpserver, drift, spec, ears, workspace) stay and gate. Exit criterion: go test ./... covers every package; new code lands only with its dedicated test.
