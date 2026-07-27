---
schema: v1
---

## B-01KYHHCFCWEV48MC3MHT3M0PZY wt.Add silently force-removes a stray worktree directory that has no ledger row
kind: bug
state: draft
created: 2026-07-27
targets: internal/wt/wt.go

FOUND by grill-abort-1 census (B-01KYHC7APA round): wt.Add (internal/wt/wt.go ~245-252) hits an existing directory at the target path with NO coord.Worktree record and clears it via git worktree remove --force with an os.RemoveAll fallback - ungated by any dirty check. Precondition differs from the identity-rotation class: no ledger row means no holder to name, but the directory can still hold a crashed agents uncommitted work (ledger write happens after wt.Add succeeds, so a crash between Add and the coord write leaves exactly this state). EXPECTED: wt.Add refuses on a non-empty stray directory containing non-.spectackle files, naming the path and the explicit recovery (delete it manually or work op=start force=true threading a force flag down); empty or records-only strays may clear silently. FIX: reuse wt.DirtyFiles semantics on the stray dir when it is a git worktree; plain non-git directories with files refuse unconditionally. TEST: wt package unit tests - stray dir with a file refuses naming the path; empty stray clears; force clears. VERIFY: go build ./... && go test ./internal/wt/ ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: wt.Add and its callers force-threading. ROLLBACK: revert.
