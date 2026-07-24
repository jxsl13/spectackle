---
schema: v0
---

## P-0020 store write batching: cold IndexAll must not pay one commit per file
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/store/store.go, go:index.parseCached

R-0003 measured 21-24s cold for 5000 files - one synchronously committed Exec per Put. Add Flush() error to store.Store (mem no-op); sqlite store buffers Puts in a single transaction committed on Flush/Close; IndexAll calls Flush once after the parse loop. Target: cold 5000-file synth tree well under 3s for the store portion (bench proof).

## T-0035 store.Flush batching + bench proof
kind: task
state: active
created: 2026-07-24
parent: P-0020

Scope ONLY: internal/store/store.go + store_test.go, internal/index/indexer.go (one Flush call after the parse loop in IndexAll) + indexer_cache_test.go if assertions need it. Interface: add Flush() error to Store; mem: no-op; sqlite: Put appends into an open tx (lazily BEGIN on first Put; guard with the existing mutex), Flush commits + clears, Close flushes then closes, Get must read-through the open tx (same connection, SetMaxOpenConns(1) already ensures serialization - verify Get inside an open tx sees buffered rows or flush-before-get). Tests: existing suite green; new: many Puts then Flush persists across reopen; Get after Put before Flush returns the entry; bench-style test (guarded by testing.Short skip) writing 5000 entries: batched must be >5x faster than the old per-Put pattern OR simply assert wall time < 3s. Verify: go build ./... && go test ./internal/store/ ./internal/index/ -race green.
