---
schema: v0
---

## P-0077 drift classification is unsound under the resident server: three confirmed defects
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/drift/drift.go, internal/mcpserver/tools.go, cmd/spectackle/main.go

Three related defects, all reproduced against this repository during the harvest of T-0102, T-0099 and T-0105. They matter more now than they did a week ago because the resident HTTP service is now the recommended operating mode, and one of the three is invisible under per-call stdio.

DEFECT 1, stale graph produces false heals. The symbol graph is rebuilt only at server startup and on reroot (mcpserver.Server.reindex); SPX-MCP-003 refreshes only the .spectackle files. Under -http a code edit made outside the server's knowledge is therefore classified against stale node positions: SpanHash is computed from the OLD Line/EndLine over the NEW file content, which yields a hash for a span that is not the node. Measured twice on go:main.main, whose body never changed while the file grew above it: stored ff8dc53a, computed 2fd917f5 (the hash of the stale line range), classified evolved, auto-healed. A second occurrence after further edits produced 0ed8d015 by the same mechanism. Both times the correct value was ff8dc53a, and both times a server restart plus check restored it. This is the exact failure the audit gate exists to prevent: a silent stamp asserting that code was reviewed when it was not.

DEFECT 2, the anchor's end line is written but never verified. Classify compares n.File == a.File and n.Line == a.Start; End is excluded from every branch. A span whose end moved therefore stays OK forever while the record shows a wrong range. Live instance in this repo right now: RSV-001 records internal/resolve/resolver.go:36-44 while go:resolve.Default actually ends at 45, and the stored hash f208f998 is the hash of 36-45, not of the range printed next to it. check reports ok. Anyone reading the record is told the wrong lines.

DEFECT 3, the reindex subcommand does not reindex the code graph. It re-syncs only the spec/doc cache via internal/sync, while the graph that drift classification depends on is built exclusively inside the MCP server. Its name and its help text both promise otherwise. This is why the obvious operator response to defect 1 does not work; only restarting the server does.

Defects 1 and 3 compound: the resident mode makes staleness possible, and the tool that ought to cure it does not. Defect 2 is independent and can be fixed without either.

Rejected: making check re-index unconditionally on every call. Indexing this repository costs a full file walk; paying it per tool call would undo the reason the resident service exists. The fix belongs at the boundary where files change, not in every consumer.

## T-0107 drift: end-line comparison, staleness guard, and a reindex that reindexes
kind: task
state: active
created: 2026-07-24
parent: P-0077
targets: internal/drift/drift.go, internal/drift/drift_test.go, internal/mcpserver/tools.go, cmd/spectackle/main.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Fix three confirmed defects in drift classification. All three were reproduced against this repository; the reproductions are given below so you can verify the fix rather than trust the description.

SCOPE (disjoint, lease exactly these four)
  internal/drift/drift.go
  internal/drift/drift_test.go
  internal/mcpserver/tools.go        staleness plumbing into the check handler only
  cmd/spectackle/main.go             reindex subcommand only
Do NOT touch internal/knowledge (a sibling task owns that whole new package), internal/mcpserver/server.go beyond what the staleness signal strictly requires, README.md, or docs/. .spectackle files are server-owned: never edit them by hand — and in particular never hand-repair anchors.tsv to make a test pass.

DEFECT 2 FIRST — it is independent and cheap, do it before the others.
Classify (internal/drift/drift.go, the switch near the end) compares n.File == a.File && n.Line == a.Start when deciding OK versus Moved. Anchor.End is written by Stamp but never read by any branch, so an anchor whose end line drifted stays OK forever while printing a wrong range.
Live reproduction in this repo: RSV-001 records internal/resolve/resolver.go:36-44; go:resolve.Default actually spans 36-45; the stored hash f208f998da2717ed is the hash of 36-45, not of 36-44; check reports ok.
Fix: include the end line in the OK criterion so a pure end-line drift classifies as Moved (position-only, refreshed silently) rather than OK. Do not change what Moved means for callers.
Contract: DRF-002 (read it with get id=DRF-002).

DEFECT 1 — stale graph produces false heals.
The graph is rebuilt only in mcpserver.Server.reindex (startup and reroot). SPX-MCP-003 refreshes .spectackle files only. Under the resident -http server, a code edit the server did not observe leaves node Line/EndLine stale, and SpanHash then hashes the NEW file at the OLD line range — a hash for a span that is not the node.
Measured twice on go:main.main, whose body never changed while the file grew above it: stored ff8dc53a80235df9, computed 2fd917f5bdf2bc9d (that is the hash of the stale range applied to the new content), classified Evolved, auto-healed. A later edit produced 0ed8d015 by the same mechanism. Both times ff8dc53a80235df9 was correct and a server restart plus check restored it.
Fix: give Classify a way to know the graph is older than the file, and make it yield Pending rather than a hash-based verdict in that case. Concretely, Classify already takes ws and g; add a staleness predicate — for example a func(file string) bool the caller supplies, or an indexedAt timestamp compared against the file's ModTime. Pick one, justify it in a comment, and keep the existing call sites compiling (a nil/zero value must mean 'no staleness information', preserving today's behavior for callers that cannot supply it). The check handler in internal/mcpserver/tools.go is the one caller that CAN supply it: the server knows when it last indexed.
Contract: DRF-003 (read it with get id=DRF-003).
Pending is the right output, not a new class: a pending anchor is exactly 'cannot judge yet', which is what a stale graph means, and existing callers already handle it. Do NOT auto-heal a Pending.

DEFECT 3 — reindex does not reindex.
cmd/spectackle's reindex subcommand re-syncs only the spec/doc cache via internal/sync; the symbol graph that drift depends on is built exclusively inside the MCP server, so the obvious operator response to defect 1 does nothing. Its help text promises otherwise.
Fix: make reindex actually rebuild the symbol graph for the given root, and print the same counts Server.reindex logs (files, nodes, edges) so an operator can confirm it ran. Reuse the existing pipeline rather than duplicating the parser list — if that requires exporting a small function from internal/mcpserver or moving the pipeline construction into a shared place, do the minimal version and say what you moved. Update the usage() text and the package doc comment.
If reusing the pipeline turns out to require restructuring more than one function, STOP and report rather than refactoring broadly — the other two fixes are independently valuable and must not be held hostage.

TESTS (internal/drift/drift_test.go, table-driven where the file already is)
  1. end-line drift: anchor End=44, node EndLine=45, identical content hash for the node's real span -> Moved, not OK. This is the RSV-001 case.
  2. exact match on file, start AND end -> OK.
  3. staleness: a predicate reporting the file as newer than the index -> Pending, and assert NO hash comparison happened (e.g. an anchor whose CHash could not possibly match still yields Pending, not Evolved).
  4. no staleness information supplied -> behavior identical to today (add this as a regression guard for existing callers).
  5. the false-heal scenario end to end at the drift level: node positions stale by four lines, file content unchanged at the node's real location -> must NOT classify Evolved.
For the reindex change, add or extend a cmd/spectackle test asserting the subcommand prints non-zero node and edge counts for a workspace containing at least one Go file.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/drift/... -race -v
  go test ./cmd/spectackle/... -race
  go test ./...
  go vet ./internal/drift/... ./internal/mcpserver/... ./cmd/spectackle/...
  /home/user/spectackle/bin/spectackle lint
Then the real-world proof, which is the point of the whole task: build your binary, run reindex against your worktree, and confirm it prints file/node/edge counts. Report those numbers.

EXIT CRITERION
All five drift tests green under -race, the cmd test green, ./... green, vet clean, lint clean, and reindex printing real counts. If any pre-existing test asserts the old OK-versus-Moved behavior, update it and say exactly which and why — that is an intended behavior change, not collateral damage.

ROLLBACK
Three independent changes. The end-line comparison is one condition; the staleness guard is one optional parameter whose zero value preserves current behavior; the reindex change is contained in one subcommand. Any one can be reverted without the others. No schema, stored format or record migration — existing anchors.tsv rows stay readable, and an anchor with a stale End simply reclassifies as Moved on the next check and gets refreshed.

REPORT BACK
Each defect's fix, each test's real output, the reindex counts, which pre-existing tests you changed and why, and anything you deliberately did NOT do.
