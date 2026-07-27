---
schema: v1
---

## B-01KYHQ8TQ6E78VP0B74VVZZ7Z5 validate op=pack on an archived item renders bare nf journal refs instead of the suppressed-pack line
kind: bug
state: done
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/validate.go

OBSERVED (2026-07-27): validate op=pack on a just-archived item returned exactly one line - nf j:internal/wt#17 #18 #19 - exit 0. GRILL-CORRECTED MECHANISM (the first drafts index-misalignment guess was wrong; reviewer reproduced live): archival physically removes the work.md block (lifecycle archive() calls item.Remove), so item.Get can NEVER return ok for an archived item; validate()s !ok path (validate.go:660-666) goes STRAIGHT to s.nearest(id), whose FTS typo-corrector surfaces the journal docs citing the ID as j:<dir>#<n> refs - designed behavior misapplied to a legitimately-terminal item. The computed: suppressed (archived) branch at validate.go:694-696 is DEAD CODE on every real path, and TestArchivedPackSuppressed gave false confidence by Upserting an item with State archived still in work.md - a state no real transition produces. The get tool (tools.go:449-487) is the shipped precedent: on !ok it tries lifecycle.Tombstone BEFORE nearest. SAME GAP in validateVerdict (~778-784). FIX: in validate() and validateVerdict, mirror getItems pattern - on !ok try lifecycle.Tombstone(s.ws, id); if found, short-circuit to sc.record(tomb) + computed: suppressed (archived) BEFORE any diff/ref resolution, no render journal event (a tombstone has no live diff; journaling render noise for it is wrong); only then fall to nearest. Reuse the established wording. Leave the dead branch as belt-and-suspenders. TEST: TempDir fixture (no git), lifecycle.Draft + Move to archived, assert item.Get !ok (fixture honesty), validate pack renders the suppressed line, no nf prefix, no j: refs; same for op=verdict; FIX TestArchivedPackSuppressed to drive the REAL archive path instead of the impossible Upsert fixture. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: the two entry checks + tests. ROLLBACK: revert.
