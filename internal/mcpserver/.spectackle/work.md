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
