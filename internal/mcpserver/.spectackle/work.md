---
schema: v1
---

## B-01KYH8JBBSEHYVNGSTN569N6X3 work op=start on an already-started item silently wipes the worktrees uncommitted files when the agent identity rotated
kind: bug
state: done
created: 2026-07-27
rounds: 2
grilled: 2026-07-27 open=0
targets: internal/mcpserver/swarm.go

OBSERVED (T-01KYH6H9 judge a3, source-confirmed against swarm.go): work op=start twice on the same item destroys uncommitted worktree files. MECHANISM per the judges trace: every CLI process without SPECTACKLE_AGENT mints a fresh random identity (coord.GenName); reattachment to an existing worktree lease requires the identity that recorded the start, so the second start - from a rotated identity - cannot reattach, treats the worktree as stale, and recreates it over the judges uncommitted implementation. Cost in the wild: one full rewrite before the judge found the env var; work op=status SHOWED the worktree open while op=submit refused no-open-worktree - the tools disagreed. EXPECTED, layered: (1) NEVER destroy - a second start that cannot reattach REFUSES naming the holder identity and the reattach condition (set SPECTACKLE_AGENT=<holder>), never recreates over a dirty tree; recreate only over a CLEAN tree or behind an explicit op=start force=true; (2) the status/submit disagreement closes - both must resolve the lease the same way; (3) the refusal text carries the fix (the identity to set), per NODE-EDGE-001 hints-name-the-next-step. TESTS: dirty-worktree second start from a rotated identity refuses and leaves every file intact; clean-tree second start may recreate; matching identity reattaches as today; status and submit agree in the rotated case. VERIFY: build/test -race/vet/gofmt; lint; check ok; the refusal text pasted. SCOPE: swarm.go work start/status/submit lease resolution + tests. ROLLBACK: revert. REPORT: the refusal line, each test, whether force was needed anywhere.

## B-01KYHC7APAEH1TA430A8MW5JW3 work op=abort by a foreign identity destroys a dead holders dirty worktree without refusal or force
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/swarm.go

FOUND by cross-val-wipe2 round-2 audit of B-01KYH8JBB (H3): workAbort gates only LIVE holders; a rotated identity running the common abort-then-start reset on a DEAD-reading holders worktree removes the tree with uncommitted files, no dirty-guard, no holder named. abort is an explicit discard op, so this is not silent-at-start class, but it is the same loss one habit away and inconsistent with the op=start guard landed in B-01KYH8JBB. EXPECTED: foreign abort on a dead holders worktree with dirty non-.spectackle files refuses naming file count, holder, and work op=abort force=true; the holder itself may always abort its own tree (explicit intent); force discards. REUSE: wt.DirtyFiles + the exact filter/refusal shape from workStart (extract a shared helper if trivial). Fail closed on DirtyFiles error, mirroring workStart. TEST: wipeguard_test.go sibling using startWorkFixtureLive short TTL - foreign abort refuses and preserves; force aborts; holder self-abort still succeeds dirty. VERIFY: go build ./... && go test ./internal/mcpserver/ -race -run 'Abort|Orphan' -count=1 && gofmt -l . empty. SCOPE: workAbort only. ROLLBACK: revert.
