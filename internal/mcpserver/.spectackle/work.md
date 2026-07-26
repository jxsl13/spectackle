---
schema: v1
---

## B-01KYGADQPKEZT80J3Y5K977Z7C archive tombstones the item before the closure merge succeeds; a transient-red CI strands a green PR with no flow actor
kind: bug
state: draft
created: 2026-07-26
grilled: 2026-07-26 open=0
targets: internal/mcpserver/tools.go, internal/mcpserver/gitflow.go

OBSERVED (T-01KYG90P closure, 2026-07-27): the archive move journaled the tombstone and removed the item from work.md, THEN gitFlowMerge refused the merge on a transiently red head (a t.TempDir cleanup flake, rerun green minutes later). Result: the item was archived and unknown to every tool, PR 149 sat open and green, and no flow actor could complete the merge - manual gh pr merge was the only path (second escape-hatch merge after PR 142). The B-01KYG56Y stranded-PR post-condition detects this shape but cannot heal it: the merge refusal is DELIBERATE on red, and after the red clears the item is already tombstoned.

EXPECTED, the ordering fix: the archive transition must not COMPLETE until its closure merge has - on a merge refusal (red checks, conflicts, forge errors) the move REFUSES and the item stays done, so a plain retry of move to=archived re-drives the whole mechanical closure once the head is green. Implementation sketch: in the move handler, run gitFlowFor BEFORE lifecycle.Move for the archived target of git-enabled task/bug items (or: run lifecycle.Move, and on gitflow merge failure roll the journal/work.md write back within the same call - pick whichever the edge-commit ordering allows, state why). The refusal names the failing check and says retry after green. Never-active records-only closures keep their current shape (nothing to strand).
NON-NEGOTIABLE, tested: a fixture whose offline-forge merge is rigged to fail leaves the item in done (retryable) with the refusal naming the cause; a retry after unrigging completes archive + merge; the stranded-PR post-condition still fires if a future path regresses; never-active records-only closure unchanged.
VERIFY: build/test -race/vet/gofmt; lint; check ok; the rigged-failure e2e pasted.
SCOPE: move handler ordering + gitflow result propagation + tests. No lifecycle.go semantics changes beyond the refusal path.
ROLLBACK: revert.
REPORT: the chosen ordering (pre-move flow vs rollback) with the edge-commit reasoning, each test.
