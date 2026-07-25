---
schema: v0
---

## P-0080 one knowledge tool with export, merge and apply; additive writes through the existing paths
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/tools.go, internal/knowledge/artifact.go

internal/knowledge carries format, extraction, recurrence merge, LLM-authored entries and persisted conflict resolutions, but nothing in the tool surface reaches it, so the whole capability is unreachable from an agent. Close that with ONE tool, not three — the standing constraint keeps the tool count minimal and orthogonal, and export/merge/apply are three operations on one noun, exactly the shape decide, rule, lease and work already use.

Operation names follow the operator's vocabulary rather than the package's: the Go function is Extract because it walks a cascade, but from the workspace's point of view the act is an export. merge condenses several artifacts; apply folds one into this workspace.

apply is the only writing operation and it must not grow a new write path. It routes rules through the existing rule op=add composer, which lints and composes, and ADRs through the decide path, so the server's guarantee that the LLM never writes .spectackle files by hand survives untouched. Additive only: it adds what is missing and never deletes or overwrites a local specialization. Idempotent: applying the same artifact twice changes nothing the second time, which is a required test, not a hope, because a fleet-wide rollout will re-run it.

The unanchored consequence stays visible on purpose: an applied rule arrives with no applies binding, so check reports it as a coverage gap until someone binds it to real nodes. That gap list IS the adoption worklist. apply must report how many entries it added and how many gaps it therefore opened, in the same call, so nobody discovers the number by surprise later.

Brownfield reach: export must also work where there is no .spectackle bundle at all. There the input is not a cascade — the LLM surveys code, tests and docs (the manifest's BROWNFIELD IMPORT paragraph already describes the read-only fan-out) and supplies entries directly. knowledge.NewEntry already validates and keys such entries identically to extracted ones, so the tool needs an entry path for them rather than new machinery.

Rejected: a separate import tool for the brownfield case. It would duplicate validation and split one concept across two vocabularies for no gain — direct authoring is an input mode of export, not a different operation.

## T-0111 knowledge tool: export, merge, apply — one tool, additive writes, idempotent
kind: task
state: active
created: 2026-07-24
parent: P-0080
targets: internal/mcpserver/knowledge.go, internal/mcpserver/knowledge_test.go, internal/mcpserver/tools.go, internal/knowledge/apply.go, internal/knowledge/apply_test.go, docs/tools.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/knowledge's six existing files before writing anything — the package is finished and you are consuming it, not redesigning it.

GOAL
Expose internal/knowledge through ONE tool named knowledge with op=export|merge|apply. Three operations on one noun, matching how decide, rule, lease and work are already shaped. Do NOT add three tools: the standing constraint keeps the tool count minimal and orthogonal.

SCOPE (lease exactly these six)
  internal/mcpserver/knowledge.go       NEW  the tool handler
  internal/mcpserver/knowledge_test.go  NEW
  internal/mcpserver/tools.go           registration + input struct only
  internal/knowledge/apply.go           NEW  the additive fold
  internal/knowledge/apply_test.go      NEW
  docs/tools.md                         the new tool's entry
Do NOT touch internal/mcpserver/commands.go or internal/mcpserver/templates (a sibling task owns the generator right now), README.md (another sibling owns it), internal/drift, cmd/spectackle, internal/item. Do NOT modify internal/knowledge's existing six files — add apply.go alongside them. .spectackle files are server-owned: never edit them by hand.

API YOU CONSUME (fixed; read the source, do not guess)
  knowledge.Extract(c *spec.Cascade, items []item.Item, source string) (Artifact, error)
  knowledge.Merge(as ...Artifact) (Artifact, []Conflict, error)
  knowledge.NewEntry(kind EntryKind, payload Entry, assertedBy, derivedFrom []Provenance) (Entry, error)
  knowledge.Marshal / knowledge.Parse
  knowledge.Resolve / knowledge.Apply   (conflict resolution — note the name clash with YOUR new Apply-to-workspace; pick a distinct name such as ApplyTo or FoldInto and say what you chose)

OPERATIONS
op=export — produce an artifact for this workspace. Two input modes:
  (a) no entries supplied: walk the cascade and items via knowledge.Extract, source = the module path (derive it the way moduleRepoURL already does, via debug.ReadBuildInfo — reuse that helper's approach, do not hardcode).
  (b) entries supplied by the caller: this is the BROWNFIELD path, for a repository with no .spectackle bundle at all, where the LLM surveyed code/tests/docs and authored entries directly. Route every supplied entry through knowledge.NewEntry so it is validated and keyed identically to an extracted one — never accept a caller-supplied key, NewEntry does not even take one.
  Output: the marshaled artifact. Support writing to a path as well as returning inline, because a fleet workflow needs a file to move between repositories.
op=merge — parse N artifacts (paths or inline), knowledge.Merge them, return the condensate AND every conflict as dense records. Conflicts are reported, never auto-resolved.
op=apply — fold one artifact into THIS workspace. See below; this is the only writing operation.

apply — the part that must not be shortcut
Additive only: add what the workspace lacks, never delete, never overwrite a local specialization. Idempotent: applying the same artifact twice must change nothing the second time. Both are required tests, not hopes — a fleet rollout re-runs this.
NO NEW WRITE PATH. Rules go through the same composer rule op=add uses (find it in tools.go and reuse it, so linting and composition still happen); ADRs go through the decide path. The server's guarantee that .spectackle files are only ever written by server-side paths must survive this task untouched. If you find yourself calling os.WriteFile on a .spectackle file, you have taken the wrong route.
An applied rule arrives with NO applies binding, so check will report it as a coverage gap. That is correct and intended — the gap list is the adoption worklist. apply must therefore report, in the same call, how many entries it added and how many gaps it opened. Contract: MCP-009 (read it with get id=MCP-009).
Dedup on the content key, not on rule ID: the receiving repo mints its own IDs, so the same sentence arriving twice must be recognized by key.

OUTPUT GRAMMAR
Dense records in the style of docs/tools.md, never JSON objects (SPX-MCP-002). Read a few existing handlers first and match them. Trailer lines summarizing counts follow the pattern check already uses (ok healed=N audit=M).

TESTS (knowledge_test.go for the tool, apply_test.go for the fold)
  1. export with no entries over a small temp workspace: artifact carries its rules and ADRs; source is the derived module path.
  2. export with caller-supplied entries (brownfield, no bundle): entries are validated and keyed; a supplied key is impossible by construction — assert the resulting key equals what Extract would compute for the same text.
  3. merge of two artifacts: recurrence counts pooled; a same-question-different-decision ADR is reported as a conflict, not merged away.
  4. apply into an empty workspace: entries land, rules lint clean afterwards, counts reported.
  5. apply twice: the second call adds nothing and reports zero added. This is the idempotence proof.
  6. apply never deletes: a workspace rule absent from the artifact is still present afterwards.
  7. gap reporting: after apply, the reported gap count matches what check actually reports.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/knowledge/... ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/knowledge/... ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
Then exercise it live: build your binary, serve it on a probed free port, and drive export then merge then apply against a scratch workspace with the call subcommand. Paste the real transcript.

EXIT CRITERION
Seven tests green under -race, idempotence and never-deletes proven by test, ./... green, vet clean, lint clean, docs/tools.md consistent with the input struct (SPX-REPO-001), and the live transcript showing all three operations.

ROLLBACK
One new tool plus one new function in internal/knowledge. Removing the registration line and the two new files restores the prior state; the package keeps working for any other caller. No schema stamp change and no stored-record migration — apply only ever adds records through paths that already existed.

REPORT BACK
The tool's final input struct, the name you chose for the workspace-apply function and why, each test's real output, the live transcript, and anything you deliberately did NOT do. If this brief contradicts what internal/knowledge actually offers, STOP and report rather than improvising.

## T-0115 server hints when its own binary is older than the sources it serves
kind: task
state: active
created: 2026-07-24
parent: P-0083
targets: internal/mcpserver/swarm.go, internal/mcpserver/swarm_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
The server should notice for itself that it is running stale code, because this repository develops itself with itself: after a merged change, the resident binary answers plausibly from code that no longer exists, and the gap is invisible from the inside. Turn an operator discipline into a property of the system.

SCOPE (lease exactly these two)
  internal/mcpserver/swarm.go        the hint, next to the existing compact hint
  internal/mcpserver/swarm_test.go   its test
Do NOT touch internal/mcpserver/tools.go, knowledge.go or docs/tools.md — a sibling task holds all three right now and is adding a new tool. Do NOT touch the Makefile, CONTRIBUTING.md or README.md — a second sibling owns the operator-facing half of this same proposal. Do NOT touch internal/knowledge, internal/drift or cmd/. .spectackle files are server-owned: never edit them by hand.

MODEL IT ON WHAT IS ALREADY THERE
swarm.go already carries a compact hint: postCall appends a nudge when the journal crosses a configured threshold, debounced (30s) and emitted once per crossing via a remembered timestamp. Read that code first and follow the same shape — same append point, same debounce discipline, same terse record style. Two hints of the same family should not be two mechanisms.

THE CHECK
Compare the running executable's modification time (os.Executable, then Stat — resolve symlinks, since a dev binary is often one) against the newest .go file under the workspace root. Sources newer than the binary means the server is serving code that is not what the tree says.
Walk cost matters: this runs on tool calls. Reuse the workspace's existing skip logic (workspace.Root.SkipDir) so vendored trees, nested git boundaries and ignored directories are pruned exactly as every other walk prunes them — a naive walk would descend into worktrees and cache directories and report nonsense. Stop early once you have found any file newer than the binary; you need a boolean, not a maximum.
Debounce harder than the compact hint. A full walk per tool call is unacceptable; cache the verdict and re-walk at most every 30 seconds. State the interval as a named constant with the reason in a comment.

THE HINT
One dense record line naming the rebuild-and-restart command (make dev, being added by the sibling task). Emitted at most once per crossing, exactly as the compact hint is — repeated nagging on every call would train the reader to ignore it, which is worse than silence.
Contract: MCP-010 (read it with get id=MCP-010).

WHAT NOT TO DO
Do not rebuild anything. A process that replaces its own binary mid-session invalidates every in-flight lease and worktree, and a failed build leaves the agent with no server at all. Reporting is safe; self-surgery is not.
Do not add a file watcher. It needs a dependency and a background goroutine to answer a question a stat already answers, and the answer only matters when someone is using the server.
Do not fail or degrade any tool call because of staleness. This is information, not a gate.

TESTS (swarm_test.go)
  1. binary newer than all sources: no hint.
  2. a source file newer than the binary: exactly one hint, naming the command.
  3. once per crossing: a second call inside the debounce window adds no second hint.
  4. the walk honors the workspace skip rules — a newer file inside a directory that SkipDir prunes must NOT trigger the hint. This is the one that catches a naive walk.
  5. os.Executable failing or the binary being unstattable degrades to silence, never to an error on the tool call.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
Then prove it live: build a binary, start it resident, touch a .go file under the root, make one tool call with the call subcommand, and show the hint appearing exactly once across two consecutive calls. Paste the transcript.

EXIT CRITERION
Five tests green under -race, the skip-rule test passing (a naive walk fails it), ./... green, vet clean, lint clean, and the live transcript showing one hint and not two.

ROLLBACK
One hint function, its cached verdict, and one append site next to an existing one. Removing them restores the prior behavior exactly; no schema, stored format, record or anchor changes, and no tool result depends on the hint.

REPORT BACK
The hint's exact text, the debounce constants and their reasoning, each test's real output, the live transcript, and anything you deliberately did NOT do.

## B-0002 server never rebinds an existing worktree: wtItem is set only by in-session work op=start, so per-call stdio clients and crashed servers cannot submit
kind: bug
state: active
created: 2026-07-25
targets: internal/mcpserver/server.go

DEFECT
wtItem (the session's open-worktree binding) is assigned only inside reroot(), reached only from work op=start. A fresh server process started with -root <worktree-root> resolves main for coordination but leaves wtItem empty, so work op=submit answers ! ARG E no open worktree even though the coord DB has the worktree record, the lease identity matches, and the tree is ready. Consequences observed on this repo: (1) the documented headless mode (one server spawn per call subcommand) cannot drive start->implement->submit at all, because the submit lands in a different process than the start; (2) a resident server that dies with an open worktree (crash, restart) orphans it permanently — T-0114 got stuck in state=replaying with no recovery path except abort, which discards the worktree's record delta.

CAUSE
Worktree membership is discoverable at startup (coord worktree records carry Root and Agent; workspace detection already resolves main from a linked worktree) but New() never consults it.

FIX (decision)
In New(), after the coord DB opens: when ws is not main, look up the coord worktree records and, if one's Root equals ws.Dir and its Agent equals this session's agent, adopt it — set wtItem so state renders wt:<item> and submit/abort address the right worktree. Identity must match: recovering a DEAD sibling's worktree stays an explicit work op=abort decision, never an implicit adoption. Rejected: rebinding on every preCall (worktree records change rarely; startup is the only ambiguous moment). Rejected: allowing adoption with mismatched agent when the holder is dead — silent takeover would race a concurrent abort and hide identity bugs.

VERIFY
go test ./internal/mcpserver/... -race with a new test: server A starts a worktree for an item; a second Server constructed with root=worktree path and the same agent binds wtItem (and a mismatched-agent server does not); go test ./...; live: T-0111/T-0115 submitted from fresh stdio processes.

ROLLBACK
One startup lookup in New(); removing it restores prior behavior. No schema, record or coord format change.

## B-0003 workAbort journals into a context dir named after the item: journal.Append gets w.Item where every other call site passes it.Dir
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/swarm.go

DEFECT
workAbort's journal.Append passes w.Item (e.g. T-0114) as the context-dir argument; workSubmit at the equivalent site passes it.Dir. Aborting a worktree therefore scaffolds <item-id>/.spectackle/ at the repo root and writes the abort event there. The stray folder then reads as a context dir (ContextDirs scans for .spectackle folders), polluting state/rules listings, and the abort event is invisible in the item's real context journal. Observed live: aborting T-0114 created T-0114/.spectackle/journal.ndjson; repaired by moving the event line into the root journal and deleting the stray dir (one-time operator repair, commit-documented).

CAUSE
coord.Worktree carries no Dir field; the author reached for w.Item. The item's Dir is available one call earlier via the item.Get already performed for the state reset.

FIX (decision)
Hoist the item.Get above the journal append, default dir to the empty root when the item is gone, and pass that dir. Add a regression test asserting no <item-id>/ directory exists at main root after an abort and that the event lands in the item's context journal. Constraint: internal/mcpserver/swarm.go is leased by T-0115's implementer right now — implement only after that lease releases.

VERIFY
go test ./internal/mcpserver/... -race including the new regression test; go test ./...

ROLLBACK
One argument change and one hoisted lookup; reverting restores prior (buggy) behavior. No schema or record format change.
