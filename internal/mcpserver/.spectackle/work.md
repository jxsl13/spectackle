---
schema: v0
---

## ADR-0012 How should spectackle coordinate agents beyond a single host: keep the central SQLite counter, or remove it in favor of coordination-free identifiers?
kind: adr
state: done
created: 2026-07-24
context: Verified current state: one coord.db at <main>/.spectackle/cache/coord.db, opened by every process via git rev-parse --git-common-dir, WAL mode, and NextID mints from a counters table with a file-scan floor so a deleted db cannot regress ids. The design is sound single-host; nothing about swarm is broken. Durable records already distribute through git (work.md, journal.ndjson, spec.md); coord.db is explicitly ephemeral, holding leases, heartbeats, the event log and the counters. Only the counters genuinely require a single serialization point. Correction to a premise raised during the discussion: plain SQLite over a network mount does NOT provide single-host guarantees, because its locking depends on filesystem advisory locks that many NFS implementations implement incorrectly; upstream warns against multi-host use. Distributed guarantees require a SQLite-compatible replicated system (rqlite, LiteFS, Turso/libSQL) or a different database. dqlite is excluded by the standing pure-Go constraint. coord already speaks database/sql, so a driver/DSN swap is far cheaper than an interface abstraction.
decision: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable
consequences: Deciding constraints named by the user: pure Go, no cgo (excludes dqlite and any C-linked backend), and lease correctness under partition (a lease two hosts both believe they hold is worse than no lease, so TTLs become load-bearing and an eventually-consistent store is not acceptable for leases). UUIDv7 gives creation-ordered, lexicographically sortable ids without any coordination, which is what removes the counter as the single serialization point. Unresolved and load-bearing: every record, doc, test and the token-economy argument assume short sequential ids like P-0084 and MCP-013; a 36-character id in every record line is a direct cost to the output diet this project treats as a feature. The proposed resolution is a git-style short rendering -- UUIDv7 as the canonical identity, a short prefix as the displayed form, lengthened locally on collision -- which keeps both properties, but it was not among the options offered and needs confirmation before implementation. Leases still need a correct store under partition; UUIDv7 solves identity, not mutual exclusion, so that half remains open.
status: accepted

kind: radio
option: keep the central SQLite counter and stay single-host
option: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable
option: abstract coord behind a Go interface with multiple backends
blocks: P-0088
choice: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable
