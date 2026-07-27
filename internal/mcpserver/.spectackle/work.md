---
schema: v1
---

## B-01KYHC7APAEH1TA430A8MW5JW3 work op=abort by a foreign identity destroys a dead holders dirty worktree without refusal or force
kind: bug
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/swarm.go

FOUND by cross-val-wipe2 round-2 audit of B-01KYH8JBB (H3): workAbort gates only LIVE holders; a rotated identity running the common abort-then-start reset on a DEAD-reading holders worktree removes the tree with uncommitted files, no dirty-guard, no holder named. abort is an explicit discard op, so this is not silent-at-start class, but it is the same loss one habit away and inconsistent with the op=start guard landed in B-01KYH8JBB. EXPECTED: foreign abort on a dead holders worktree with dirty non-.spectackle files refuses naming file count, holder, and work op=abort force=true; the holder itself may always abort its own tree (explicit intent); force discards. REUSE: wt.DirtyFiles + the exact filter/refusal shape from workStart (extract a shared helper if trivial). Fail closed on DirtyFiles error, mirroring workStart. TEST: wipeguard_test.go sibling using startWorkFixtureLive short TTL - foreign abort refuses and preserves; force aborts; holder self-abort still succeeds dirty. VERIFY: go build ./... && go test ./internal/mcpserver/ -race -run 'Abort|Orphan' -count=1 && gofmt -l . empty. SCOPE: workAbort only. ROLLBACK: revert.

## B-01KYHEQG6PEBFBX49MPEZMVNH6 compact apply=true archives done items bypassing the validation gate and the atomic git closure edge
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/tools.go

OBSERVED (2026-07-27, live incident on B-01KYHCHRN0): compact path=internal/mcpserver apply=true swept the done item straight to archived via lifecycle.Move(StateArchived) (tools.go ~2352) while its PR 163 was OPEN with CI failing and NO validation verdict existed. Every guard the move tool enforces on done->archived was skipped: feedback.validate=require (no op=verdict), the atomic archive edge (gitFlowMerge never ran - no merge, no closure), and the stranded-PR post-condition (B-01KYGADQ). Result: the records/code split class (PR 142/143) reachable through a side door - records tombstoned on the serving root, code unlanded. EXPECTED: compacts done-item sweep goes through the SAME edge as move to=archived - flow guards included - or, when any gate refuses (missing verdict, failing CI, unmerged PR), the sweep SKIPS that item and renders the refusal line as its candidate entry (c done-item <id> blocked: <reason>); compaction of journal events must never be the lever that closes a lifecycle. FIX: route the sweep through the servers own archive path (the mcpserver move handler logic, not raw lifecycle.Move) or precheck verdict+flow and skip; keep dry-run listing unchanged. TEST: e2e in the offline fixture - done item without verdict + compact apply=true leaves the item done and renders the blocked line; with verdict and merged closure it archives. Also pin: a compact run never calls gitFlowMerge implicitly without the flow guards. RECOVERY of the live incident is handled outside this item (escape-hatch merge of PR 163 after independent validation, recorded in the journal). VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: compact sweep only. ROLLBACK: revert.
