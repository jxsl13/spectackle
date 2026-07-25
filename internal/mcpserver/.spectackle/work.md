---
schema: v0
---

## T-0129 tool-surface invariant: no bundle directory may appear outside a legitimate context dir
kind: task
state: approved
created: 2026-07-25
parent: P-0087
refs: B-0003
targets: internal/mcpserver/invariants_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. NEW FILE ONLY — do not edit any existing file in internal/mcpserver; siblings and future tasks touch those.

CLASS (B-0003, read it with get id=B-0003): one call site passed an item ID where journal.Append expects a context dir, which scaffolded a directory named after the item at the repo root that then read as a context dir, polluting state and rule listings. The defect is not that one argument; it is that nothing checks where bundles may exist.

THE INVARIANT
Over a scratch git-backed workspace, exercise the mutating tool surface end to end through the in-memory MCP session helpers already in this package (connectRoot/callText in tools_test.go — reuse them, do not build a new harness): draft of every kind the tool accepts, move across the forward states including rejected-with-note and archived, rule add plus edit plus retire, decide ask and answer, grill, work start then abort, work start then submit, lease claim and release, compact, and knowledge apply if it is reachable without a fixture artifact. Then walk the whole workspace tree and assert that every directory containing a .spectackle folder is a legitimate context dir: a directory that existed as a source directory before the run, or the workspace root itself. In particular no such parent may match item.IDRe — that is B-0003's exact shape and the cheapest tripwire for the whole class.

Fail with the offending path and the tool sequence that produced it, not a bare boolean, so the next occurrence is diagnosable from the failure alone. Keep the walk skipping the worktree directory the work op=start leg creates: a worktree legitimately carries its own bundles.

VERIFY (run every one, real output, never predicted)
  go build ./...
  go test <your packages> -race
  go test ./...
  go vet <your packages>
  /home/user/spectackle/bin/spectackle lint
PROVE THE TEST BITES: for each invariant, temporarily reintroduce the defect it encodes (revert the guard, restore the old gate, whatever is minimal), show the test failing, restore, show it passing. A green invariant that would also be green against the broken code is worthless; paste both transcripts.

ROLLBACK: new test files plus the bounded fix named above; each revertible on its own.
REPORT BACK: each invariant with the class it generalizes, the failing-then-passing transcript, real verify output, and anything you deliberately did NOT do.

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
