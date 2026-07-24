---
schema: v0
---

## T-0016 sqlite parse-blob store + cache-accelerated IndexAll
kind: task
state: done
created: 2026-07-24
parent: P-0011

Scope ONLY: internal/store/store.go + store_test.go, internal/index/indexer.go (parseCached + doc) + new indexer_cache_test.go. Store: keep interface+mem; add Open(path) (Store, error) using modernc.org/sqlite (already a module dep), table blobs(path TEXT PRIMARY KEY, hash BLOB, blob BLOB), Get compares stored hash, Put upserts, Close closes; store stays a dumb KV - gob encode/decode happens in index.parseCached (index imports store, never the reverse). parseCached: hash=sha256(src); Get hit -> gob-decode ParseResult; miss -> p.Parse, gob-encode, Put (Put errors are non-fatal: log-free skip). Tests: roundtrip, hash-mismatch miss, reopen persistence; counting-parser wrapper proves 2nd IndexAll with same store does 0 parses and yields identical node/edge counts. NO server.go changes.
