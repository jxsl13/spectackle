---
schema: v1
---

## B-01KYHHCFCWEV48MC3MHT3M0PZY wt.Add silently force-removes a stray worktree directory that has no ledger row
kind: bug
state: active
created: 2026-07-27
grilled: 2026-07-27 open=4
targets: internal/wt/wt.go

FOUND by grill-abort-1 census (B-01KYHC7APA round): wt.Add (internal/wt/wt.go ~245-252) hits an existing directory at the target path with NO coord.Worktree record and clears it via git worktree remove --force with an os.RemoveAll fallback - ungated by any dirty check. The state is reachable: the ledger write happens after wt.Add succeeds, so a crash between Add and the coord write leaves exactly this orphan. EXPECTED: wt.Add refuses on a stray directory containing non-.spectackle files, naming the path and the recovery (force flag threaded from work op=start force=true); empty or records-only strays clear silently; plain non-git directories with files refuse unconditionally; fail closed when dirt is unreadable (mirror dirtyOrphanGuard semantics). FIX: wt.Add gains a force parameter (or a sibling AddForce kept minimal); the swarm.go call site (the 1/7 divergent site the pack flags at swarm.go:729) threads the callers force. GRILL PACK ADDRESSED: the two unconsumed-symbol findings (go:wt.DeleteBranch, go:wt.IsAheadOf) are package-internal plumbing consumed at wt.go:438 (DiscardBranch) and wt.go:178-180 (IsAheadOfRemote) - census artifacts, no action; the two tests=absent computed findings (B-0004 branch-name, T-0136 path-normalization) are pre-existing anchor coverage gaps in this dir, deliberately OUT of this fix (separate concern, would dilute the diff) - noted here so the deferral is a decision, not an oversight. TEST: wt unit tests - stray git-worktree dir with an agent file refuses naming the path; empty stray clears; records-only stray clears; force clears; unreadable stray refuses. VERIFY: go build ./... && go test ./internal/wt/ ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: wt.Add + the one swarm call site + tests. ROLLBACK: revert.
