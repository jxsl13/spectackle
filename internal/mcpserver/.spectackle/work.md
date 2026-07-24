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

## P-0089 compact reports redundant rejection clusters; draft and rule stop implying IDs are predictable
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/tools.go

Two changes in the file that was held for three consecutive rounds, both already analyzed and both blocked on nothing but access.

First: internal/journal now clusters redundant rejections, selects a canonical event, represents supersession and retargets citations — all as pure functions, with an invariant guard proving no event can be dropped. Nothing calls any of it. The compact tool is where it belongs, next to the mergeable-rule pairs it already reports, and the shape is the same: the dry-run names clusters, applying stays a deliberate second act. The reason to keep that separation is stronger for rejections than it was for rules — a wrongly merged rule can be split again, while a wrongly superseded rejection hides a lesson exactly when someone is about to repeat it.

Second: draft says the server assigns IDs and rule says auto-IDs, which is true and insufficient. Neither says the counter is shared across every process and worktree through one coord.db, so the next number cannot be derived by looking at the highest one visible and gaps are normal. A caller reading those descriptions can reasonably believe otherwise. Two task bodies in this repository cited contract IDs guessed that way and were rejected for it, the second after the first lesson had already been recorded — which is what makes this a tool-surface gap rather than only an operator error. The information was reachable: swarm shows sibling mints in realtime, and one implementer resolved the same question correctly by querying coord.db directly. The descriptions should say so.

Rejected: minting IDs client-side to make them predictable. The shared counter is the only collision-free mint across worktrees, and predictability is not worth surrendering that.

## T-0123 compact reports redundant rejection clusters; draft and rule describe the shared ID counter
kind: task
state: active
created: 2026-07-24
parent: P-0089
targets: internal/mcpserver/tools.go, internal/mcpserver/tools_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/journal/redundant.go before writing anything — it is finished, tested, and you are calling it, not redesigning it.

TWO CHANGES, both in tools.go. Do them in either order.

SCOPE (lease exactly these two)
  internal/mcpserver/tools.go
  internal/mcpserver/tools_test.go
Do NOT touch docs/tools.md — a sibling task owns it right now and is bringing it back in sync with the ALREADY-SHIPPED surface. Your new compact records are NOT shipped yet, so they must not be documented there in this round; note in your report that docs/tools.md needs a follow-up entry for them. Do NOT touch internal/journal (finished), internal/knowledge, internal/item, internal/drift, cmd/, README.md, swarm.go, grill.go, decide.go, commands.go. .spectackle files are server-owned: never edit them by hand.

CHANGE 1 — compact reports redundant rejection clusters
internal/journal provides, already tested: ClusterRedundant(events) []RedundancyCluster (note-only sentence-token Jaccard at 0.6, returns SINGLETON clusters too, canonical = earliest by timestamp then event id), RedundancyCluster{Canonical, Superseded}, NewSupersession, Supersession.ToEvent, RetargetCitations. Read the file for exact signatures rather than trusting this summary.
Wire the DRY-RUN only. The compact handler already reports mergeable rule pairs as c records; add redundant rejection clusters beside them, in the same style.
Contract MCP-014 (read it with get id=MCP-014): WHEN the compact dry-run runs, the server SHALL report each redundant rejection cluster as one `c <dir> redundant <canonical>+<superseded>` record and never supersede automatically.
Singleton clusters are not redundancy — report only clusters with at least one superseded member, or the output fills with noise for every rejection that resembles nothing.
Do NOT implement the apply path in this task. Superseding is a deliberate second act, and the reason is stronger here than for mergeable rules: a wrongly merged rule can be split again, while a wrongly superseded rejection hides a lesson exactly when someone is about to repeat it. If you find yourself writing a journal write, stop.
Budget: compact is already a whole-journal pass. Do not add a second read of the same events — reuse what the handler already loaded if it is in scope, and say what you did.

CHANGE 2 — draft and rule stop implying IDs are predictable
Both tool descriptions in tools.go say the server assigns IDs (draft: server assigns ID+scope; rule: auto-IDs). True and insufficient. Neither says the counter lives in ONE coord.db shared by every process and worktree, so the next number cannot be derived from the highest one currently visible, and gaps are normal (a sibling may mint, or retire, between your two calls).
Add that to both descriptions, tersely, in the register those descriptions already use. Say what a caller should do instead: read the id the call RETURNS, or consult swarm, which shows sibling mints in realtime.
This is not cosmetic. Two task bodies in this repository cited contract IDs guessed from the highest visible number and were rejected for it, the second after the first lesson had been recorded.

TESTS (tools_test.go — extend, do not add parallel files)
  1. two reject events with near-identical notes produce one `c <dir> redundant` record naming canonical and superseded.
  2. two unrelated rejections produce no redundant record.
  3. a singleton cluster produces no record.
  4. the dry-run writes nothing: assert the journal is byte-identical before and after the call. This is the load-bearing test — it is what proves compaction did not quietly supersede.
  5. both descriptions contain the shared-counter wording; assert on a substring that would survive rewording but not deletion.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
Then prove it live: build your binary, serve it on a probed free port against a scratch workspace, create two items, reject both with notes stating the same lesson in different words, and run compact's dry-run. Paste the transcript showing the redundant record and an unchanged journal.

EXIT CRITERION
Five tests green under -race, the writes-nothing test passing, ./... green, vet clean, lint clean, and the live transcript.

ROLLBACK
One report section and two description strings. Removing them restores compact's current output exactly; no schema, stored format, record or anchor change, and nothing in internal/journal is touched.

REPORT BACK
The record format, whether you reused the already-loaded events or read again, the exact description additions, each test's real output, the live transcript, and anything you deliberately did NOT do.
