---
schema: v1
---

## ADR-01KYGCJ70JESXTEGZJVWF7AXN4 v0.2.0: the full chain landed early - cut the release now or hold to the planned Aug 1-2 window?
kind: adr
state: done
created: 2026-07-26
decision: cut now
status: accepted

kind: radio
option: cut now
option: hold to Aug 1-2
choice: cut now

## T-01KYHAH1GJEFZ861R0NGT9W8PV offline mode is single-branch commits only: no PRs, no pushes, no branch dance
kind: task
state: draft
created: 2026-07-27
targets: internal/mcpserver/gitflow.go, internal/forge/offline.go, docs/lifecycle.md

USER REQUIREMENT (2026-07-27, verbatim intent): offline mode must not create any PRs and not push any git changes - it works on a SINGLE branch and creates commits, nothing else. TODAY offline simulates the full online shape: item branches, offline:// PR records (draft/ready/merge), local base merges - a paper theater of the forge. The requirement collapses it to the honest minimum: every lifecycle edge commits directly on the current branch (edge commits + records/code sweeps as today), and the branch/PR/merge machinery is BYPASSED entirely under mode: offline.

WHAT CHANGES: gitFlowStart/Ready/Merge under offline: no EnsureBranch, no forgeFor, no PR records - the start edge commits the title commit on the CURRENT branch, done commits the sweep, archive commits the records; render honest lines (g offline commit <short-sha> <subject>) instead of pr theater. gitPush is never called offline (verify it already refuses/skips; make it structural). The offline forge type stays for TESTS that exercise the online shape locally (the e2e suite depends on it) - it is no longer reachable from mode: offline; document that split in offline.go and lifecycle.md. The work op=start worktree flow under offline: worktrees still function (they are local), but their submit integrates by COMMIT on the single branch, not merge - verify what submit does offline today and align. CONFIG: no new keys - mode: offline simply means what it says now.
MIGRATION/TESTS: every existing test that preps offline fixtures and asserts pr/merged lines moves to asserting the new g offline commit lines OR switches its fixture to the test-forge path explicitly - enumerate them (the archive-flow e2es, redraft cycle, atomic-edge, interleaved drive) and state per test which route it took and why. Never-silent holds: each edge still renders its commit.
NON-NEGOTIABLE: under mode: offline a full lifecycle (draft->archive) produces ZERO branches beyond the current one, ZERO pr records, ZERO push invocations (assert via a git remote-less fixture AND a push-spy), and a linear commit chain carrying edge+sweep commits; the online path is byte-identical to today (suite proves).
VERIFY: build/test -race/vet/gofmt; lint; check ok; the offline lifecycle commit log pasted.
SCOPE: gitflow offline arms + docs + test migration. No forge interface changes.
ROLLBACK: revert.
REPORT: the commit-log paste, the per-test migration table, anything the single-branch model makes impossible (say it, do not approximate).

## B-01KYHBV5QBEJWRW3MJ96JD2M7Y releasenotes renders an item once per archive event: a reopened-and-rearchived item appears twice
kind: bug
state: draft
created: 2026-07-27
targets: internal/relnotes/relnotes.go

OBSERVED (v0.2.1 prep, 2026-07-27): spectackle releasenotes -since v0.2.0 lists T-01KYGX9P twice under Features - the item was reopened and re-archived, producing two EvArchive journal events, and relnotes.Render groups raw events without deduplication by ID. EXPECTED: one line per item; the LAST archive event wins (its tombstone note is the final one). FIX: in Render, dedupe byKind entries by e.ID keeping the latest T before sorting. TEST: extend TestRenderGolden with two EvArchive events for one ID at different T carrying different Sum - exactly one line renders and it carries the later Sum. VERIFY: go build ./... && go test ./internal/relnotes/ -count=1 && gofmt -l . empty. SCOPE: relnotes.go only. ROLLBACK: revert.
