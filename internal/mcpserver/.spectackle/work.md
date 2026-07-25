---
schema: v0
---

## T-0130 worktree submit end to end under the four conditions the old tests never had
kind: task
state: approved
created: 2026-07-25
parent: P-0087
refs: B-0002, B-0004, B-0005, B-0006
targets: internal/mcpserver/worktree_e2e_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. NEW FILE ONLY — do not edit any existing file in internal/mcpserver.

CLASS: four separate defects (B-0002, B-0004, B-0005, B-0006 — read all four) hid behind one gap in the test bed, which only ever exercised a repository whose primary branch was literally named main, whose main never advanced while a worktree was open, whose worktrees carried no live record state, and whose submit ran in the process that opened the worktree. Every one of those is false in real use.

THE TEST: one end-to-end that violates all four at once.
  1. Build a git-backed workspace whose primary checkout is on a branch NOT named main (a stale branch literally named main may also exist, diverged — that is what made B-0004 invisible). Reuse this package's existing git fixture helpers rather than writing new ones.
  2. Open a worktree for an approved item via work op=start, and make a real code change inside it.
  3. Advance the primary checkout: commit a change that touches the SAME .spectackle journal paths the worktree carries live and uncommitted (B-0006's exact collision).
  4. Submit from a FRESH server instance rooted at the worktree path, not the one that started it (B-0002's cross-process rebinding), with the same agent identity.
  5. Assert: the submit merges, the primary branch (not the stale main) advanced to carry the code change, the item's record state landed on the primary side, and the worktree's live record delta was not lost.
Also assert the record-only case explicitly (B-0005): a second submit attempt whose only remaining difference is bundle state must not fail on an empty commit.

If any step needs a helper that does not exist yet, put it in your own new file. If the assertion cannot be made without touching an existing file, STOP and report rather than editing one.

VERIFY (run every one, real output, never predicted)
  go build ./...
  go test <your packages> -race
  go test ./...
  go vet <your packages>
  /home/user/spectackle/bin/spectackle lint
PROVE THE TEST BITES: for each invariant, temporarily reintroduce the defect it encodes (revert the guard, restore the old gate, whatever is minimal), show the test failing, restore, show it passing. A green invariant that would also be green against the broken code is worthless; paste both transcripts.

ROLLBACK: new test files plus the bounded fix named above; each revertible on its own.
REPORT BACK: each invariant with the class it generalizes, the failing-then-passing transcript, real verify output, and anything you deliberately did NOT do.
