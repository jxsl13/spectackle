---
schema: v1
---

## B-01KYHQ8TQ6E78VP0B74VVZZ7Z5 validate op=pack on an archived item renders bare nf journal refs instead of the suppressed-pack line
kind: bug
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/validate.go

OBSERVED (2026-07-27, B-01KYHHCFCW immediately after its archive): validate op=pack id=<archived item> returns exactly one line - nf j:internal/wt#17 j:internal/wt#18 j:internal/wt#19 - and nothing else, exit 0. EXPECTED per the archived-pack convention (compaction-survival work): archived items render computed: suppressed (archived) or an explicit item-is-archived refusal; bare nf nearest-match refs are the not-found grammar for lookups and tell the caller nothing about WHY the pack is empty. LIKELY MECHANISM: the pack resolves the item to its tombstone and its verdict-ref resolution walks journal indexes that no longer line up post-archive (or post-fold); the nf fallback then swallows the real answer. FIX: validate op=pack on a terminal item short-circuits to an honest line naming the state (i <id> archived - pack suppressed; verdicts live in the journal tombstone) before any ref resolution. TEST: e2e - archive an item through the offline fixture, call validate op=pack, assert the suppressed line and NO nf output. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 -run Validate && gofmt -l . empty. SCOPE: the pack entry path only. ROLLBACK: revert.

## B-01KYHV740TE8Q8KHKCV95VC3JS offline archive gate is weaker than the online draft-flip it mirrors: red-gated done reads as passing, and everActive dies with journal compaction
kind: bug
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/gitflow.go, internal/mcpserver/validate.go

FOUND by cross-val-offline (PR 167 round, H1+H2, non-blocking there). H1: lastGateResult (validate.go) reads ANY EvMove->done as last=pass, but the offline done edge moves the item to done even on a RED gate (only a ! GATE E note renders; nothing journals a gate-fail marker) - so a red-gated item archives UNGATED, where online the same items PR stays draft and the merge arm re-runs the gate and refuses. The PR 167 P4 e2e demonstrates it: verify test -f ok.txt is red the whole run yet the archive completes. Never-silent holds; gate-strictness does not. H2: everActive walks EvMove events, and journal compaction folds ALL EvMove including live items - an item activated more than JournalMax events before a compact sweep reads never-active and the offline archive gate silently skips (lastGateResult shares the exposure; its online failure direction was benign). FIX: (1) the offline done edge journals a machine-readable gate outcome (extend the EvMove note with the existing gate fail token on red, or an explicit event kind that the compaction keep-list already preserves - EvReview/EvValidate class); lastGateResult keys on that token, making red-gated done read fail; (2) the offline archive arm re-runs the gate whenever the last outcome is not an explicit pass (mirroring the online draft-flip exactly, not approximately); (3) compaction keeps the LAST activation and LAST gate-outcome event per live item (terminal items unaffected) so everActive and lastGateResult survive folds - or both helpers fall back to the item records rounds/state header when the journal is silent. TEST: red-gate done then archive offline refuses and re-runs the gate; green retry archives; compact apply=true between activation and archive does not skip the gate (fold fixture with journal_max=1). VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: the two helpers + offline arm + compaction keep-list + tests. ROLLBACK: revert.
