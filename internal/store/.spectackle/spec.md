---
schema: v1
---

## intent
- T-01KY9M5PQ0F1GRYE9D5CBNTWYR store.Flush batching + bench proof: Scope ONLY: internal/store/store.go + store_test.go, internal/index/indexer.go (one Flush call after the parse loop in IndexAll) + indexer_cache_test.go if assertions need it. Interface: add Flush() error to Store; mem: no-op; sqlite: Put appends into an open tx (lazily BEGIN on first Put; guard with the existing mutex), Flush commits + clears, Close flushes then closes, Get must read-through the open tx (same connection, SetMaxOpenConns(1) already ensures serialization - verify Get inside an open tx sees buffered rows or flush-before-get). Tests: existing suite green; new: many Puts then Flush persists across reopen; Get after Put before Flush returns the entry; bench-style test (guarded by testing.Short skip) writing 5000 entries: batched must be >5x faster than the old per-Put pattern OR simply assert wall time < 3s. Verify: go build ./... && go test ./internal/store/ ./internal/index/ -race green.
- P-01KY9M5PQ0F75A0GXHR7TYNRJB store write batching: cold IndexAll must not pay one commit per file: flush batching landed with T-01KY9M5PQ0F1GRYE9D5CBNTWYR: 11.3x faster cold IndexAll

## STO-001 {applies: go:store.Store}
WHEN `Put` is called outside an open transaction, the parse-blob store SHALL open a lazy write transaction that `Flush` or `Close` commits, while `Get` reads through the open transaction uncommitted.
