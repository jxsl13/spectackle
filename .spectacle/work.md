---
schema: v0
---

## P-0011 persistent parse-blob store: warm graph start across sessions (M2 slice)
kind: proposal
state: approved
created: 2026-07-24
targets: internal/store/store.go, go:index.indexer.IndexAll

store gains a SQLite-backed impl (own parse.db in .spectacle/cache, modernc driver, blobs(path PRIMARY KEY, hash, blob)); index.parseCached becomes real: sha256 hit -> gob-decode cached ParseResult, miss -> parse + Put. Second IndexAll over an unchanged tree performs zero Parse calls. IndexPaths stays a documented stub (graph has no node removal yet). Server wiring (persistent store handle in New/reindex) is orchestrator-owned, not part of the implementer task.

## T-0017 gofmt legacy debt: five files
kind: task
state: done
created: 2026-07-24

Scope ONLY: gofmt -w internal/drift/drift.go internal/ears/ears_test.go internal/ids/ids_test.go internal/resolve/resolver.go internal/spec/author.go. Verify gofmt -l on internal/ and cmd/ is empty afterwards, go vet ./... and go test ./... -count=1 stay green. No semantic edits whatsoever.
