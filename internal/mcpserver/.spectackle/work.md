---
schema: v1
---

## B-01KYHC7APAEH1TA430A8MW5JW3 work op=abort by a foreign identity destroys a dead holders dirty worktree without refusal or force
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/swarm.go

FOUND by cross-val-wipe2 round-2 audit of B-01KYH8JBB (H3): workAbort gates only LIVE holders; a rotated identity running the common abort-then-start reset on a DEAD-reading holders worktree removes the tree with uncommitted files, no dirty-guard, no holder named. abort is an explicit discard op, so this is not silent-at-start class, but it is the same loss one habit away and inconsistent with the op=start guard landed in B-01KYH8JBB. EXPECTED: foreign abort on a dead holders worktree with dirty non-.spectackle files refuses naming file count, holder, and work op=abort force=true; the holder itself may always abort its own tree (explicit intent); force discards. REUSE: wt.DirtyFiles + the exact filter/refusal shape from workStart (extract a shared helper if trivial). Fail closed on DirtyFiles error, mirroring workStart. TEST: wipeguard_test.go sibling using startWorkFixtureLive short TTL - foreign abort refuses and preserves; force aborts; holder self-abort still succeeds dirty. VERIFY: go build ./... && go test ./internal/mcpserver/ -race -run 'Abort|Orphan' -count=1 && gofmt -l . empty. SCOPE: workAbort only. ROLLBACK: revert.
