---
schema: v1
---

## B-01KYHQ8A7NFRC8RMGCDJMC6H1C wt.Add with wtRoot equal to the main checkout destroys the entire main working tree
kind: bug
state: done
created: 2026-07-27
grilled: 2026-07-27 open=4
targets: internal/wt/wt.go

FOUND and empirically confirmed by cross-val-wtadd (PR 166 adversarial pass): Add(root, root, branch, HEAD, base, false) on a CLEAN main repo passes strayGuard (DirtyFiles empty - the guard classifies dirty vs clean, never is-this-the-main-root), proceeds into the removal step, and deletes the entire main checkout directory; the follow-up git worktree add then errors with cannot-change-to because the directory is gone. Pre-existing (the pre-guard code had the same unconditional removal), severity high because the loss is the WHOLE repo working tree including untracked work. Reachability from workStart depends on id/WtDir construction never producing wtRoot==mainRoot - no validator enforces that today. EXPECTED: Add refuses outright when wtRoot resolves to the same path as mainRoot (or to any registered non-linked checkout root), before ANY removal, force notwithstanding - self-destruction is never a valid recovery. FIX: resolve both paths (filepath.EvalSymlinks + Abs) and refuse on equality; also refuse when wtRoot/.git is a DIRECTORY (a full checkout, not a linked worktree - linked worktrees carry a .git FILE). TEST: Add(root, root, ...) refuses naming both paths, tree intact including an untracked file; symlinked wtRoot to root refuses; normal linked-worktree Add still green. VERIFY: go build ./... && go test ./internal/wt/ -count=1 && gofmt -l . empty. SCOPE: wt.Add guard only. ROLLBACK: revert.
