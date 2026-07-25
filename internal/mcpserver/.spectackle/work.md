---
schema: v0
---

## B-0003 workAbort journals into a context dir named after the item: journal.Append gets w.Item where every other call site passes it.Dir
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/swarm.go

DEFECT
workAbort's journal.Append passes w.Item (e.g. T-0114) as the context-dir argument; workSubmit at the equivalent site passes it.Dir. Aborting a worktree therefore scaffolds <item-id>/.spectackle/ at the repo root and writes the abort event there. The stray folder then reads as a context dir (ContextDirs scans for .spectackle folders), polluting state/rules listings, and the abort event is invisible in the item's real context journal. Observed live: aborting T-0114 created T-0114/.spectackle/journal.ndjson; repaired by moving the event line into the root journal and deleting the stray dir (one-time operator repair, commit-documented).

CAUSE
coord.Worktree carries no Dir field; the author reached for w.Item. The item's Dir is available one call earlier via the item.Get already performed for the state reset.

FIX (decision)
Hoist the item.Get above the journal append, default dir to the empty root when the item is gone, and pass that dir. Add a regression test asserting no <item-id>/ directory exists at main root after an abort and that the event lands in the item's context journal. Constraint: internal/mcpserver/swarm.go is leased by T-0115's implementer right now — implement only after that lease releases.

VERIFY
go test ./internal/mcpserver/... -race including the new regression test; go test ./...

ROLLBACK
One argument change and one hoisted lookup; reverting restores prior (buggy) behavior. No schema or record format change.
