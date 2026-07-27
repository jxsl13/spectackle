---
schema: v1
---

## B-01KYHQ8TQ6E78VP0B74VVZZ7Z5 validate op=pack on an archived item renders bare nf journal refs instead of the suppressed-pack line
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/validate.go

OBSERVED (2026-07-27, B-01KYHHCFCW immediately after its archive): validate op=pack id=<archived item> returns exactly one line - nf j:internal/wt#17 j:internal/wt#18 j:internal/wt#19 - and nothing else, exit 0. EXPECTED per the archived-pack convention (compaction-survival work): archived items render computed: suppressed (archived) or an explicit item-is-archived refusal; bare nf nearest-match refs are the not-found grammar for lookups and tell the caller nothing about WHY the pack is empty. LIKELY MECHANISM: the pack resolves the item to its tombstone and its verdict-ref resolution walks journal indexes that no longer line up post-archive (or post-fold); the nf fallback then swallows the real answer. FIX: validate op=pack on a terminal item short-circuits to an honest line naming the state (i <id> archived - pack suppressed; verdicts live in the journal tombstone) before any ref resolution. TEST: e2e - archive an item through the offline fixture, call validate op=pack, assert the suppressed line and NO nf output. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 -run Validate && gofmt -l . empty. SCOPE: the pack entry path only. ROLLBACK: revert.
