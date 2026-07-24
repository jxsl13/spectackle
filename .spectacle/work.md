---
schema: v0
---

## P-0001 M1 structural core: go/parser indexer + cgo edges
kind: proposal
state: draft
created: 2026-07-24
targets: internal/index/indexer.go, internal/graph/graph.go, internal/resolve/cgo.go

Implement the Go language parser on stdlib go/parser (incl. EndLine spans), populate the in-memory graph, wire the cgo resolver, and make find scope=code / get depth real. Exit: saxpy transcript #impact reproduces; anchors leave pending state.
