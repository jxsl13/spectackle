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

## P-0082 generated slash commands for the read-only operations, plus export and merge triggers
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/commands.go

Today the generator emits two commands: the lifecycle entry point and a state snapshot. Everything else — searching records, reading one by ID, the research pack, sibling awareness, and now exporting and merging knowledge — is reachable only by hand-writing a tool call. That asymmetry is backwards: the read-only operations are the ones a user reaches for most often and the ones that are safe to expose as one-liners, precisely because they write nothing.

Add generated commands for the querying surface and for the two knowledge operations a user triggers rather than authors. They share the existing generator, the existing template directory and the existing harness fan-out (claude, copilot, codex, kimi), so the marginal cost per command is one template file.

Which commands, and why these: search over records, read one record by ID, the aggregated research pack, and the swarm view are the four read-only operations with distinct arguments — state already exists. Export and merge join them because both are user-triggered rather than agent-internal: export is how a repository publishes what it knows, merge is how several such publications become one condensate. apply is deliberately NOT given a command: it writes, and a one-line slash command is the wrong front door for the only operation in this family that mutates a workspace.

Every generated file carries the do-not-edit header and is produced by the tool, never by hand — the existing invariant. SPX-REPO-001 also requires docs/tools.md to stay consistent with the tool structs, so any new command's documented arguments must match what the server actually accepts.

Rejected: one parameterized command taking the operation as its first argument. It would collapse six discoverable entry points into one that a user has to read documentation to use, which defeats the reason slash commands exist.

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

## T-0113 generated commands for find, get, research, swarm, export and merge
kind: task
state: active
created: 2026-07-24
parent: P-0082
targets: internal/mcpserver/commands.go, internal/mcpserver/commands_test.go, internal/mcpserver/templates/commands/

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/mcpserver/commands.go and BOTH existing templates before writing anything — you are extending a working generator, not designing one.

GOAL
The generator emits two commands today: the lifecycle entry point and a state snapshot. Every other read-only operation is reachable only by hand-writing a tool call. Add generated commands for the querying surface plus the two knowledge operations a user triggers.

SCOPE (lease exactly these three paths)
  internal/mcpserver/commands.go                    registration of the new template names
  internal/mcpserver/commands_test.go               assertions
  internal/mcpserver/templates/commands/*.md.tmpl   the new templates
Plus the regenerated harness surfaces, which the TOOL writes, never you: .claude/commands/*.md, .github/prompts/*.prompt.md, AGENTS.md.
Do NOT touch internal/mcpserver/tools.go or internal/mcpserver/knowledge.go (a sibling task owns both right now and is adding the knowledge tool), README.md (another sibling owns it), internal/knowledge, internal/drift, cmd/spectackle. .spectackle files are server-owned: never edit them by hand.

COMMANDS TO ADD (six templates)
  find      search records. Arguments: a query, and the scope (code|rule|spec|proposal|task|bug|research|adr|rejection|history|all). Read scopeKinds in tools.go to get the list exactly right — do not invent scopes.
  get       read one record by ID (item, rule, node, dir or file), with the optional depth for impact radius.
  research  the aggregated read-only pack for a topic.
  swarm     sibling awareness, zero arguments.
  export    trigger knowledge op=export for this workspace.
  merge     trigger knowledge op=merge over several artifacts.
Deliberately NOT given a command: apply. It is the only operation in this family that mutates a workspace, and a one-line slash command is the wrong front door for it. Say so in the export/merge templates' prose so a reader is not left wondering.

IMPORTANT COORDINATION NOTE
The knowledge tool is being implemented by a sibling task RIGHT NOW and does not exist in your worktree. Its contract is fixed and you may rely on it: tool name knowledge, op=export|merge|apply. Write the export and merge templates against that contract. Do NOT try to call the tool, do not import it, and do not add a test that invokes it — your tests assert on GENERATED TEXT, not on tool behavior.

TEMPLATE CONVENTIONS (follow the existing two exactly)
  Every generated file starts with the do-not-edit header the generator stamps.
  Templates receive commandsData{Binary, Tool, RepoURL} — use {{.Tool}} and {{.Binary}} rather than hardcoding "spectackle", and remember the naming convention: the workflow command is <binary>.md and the others are <binary>-<name>.md.
  Each template tells the agent which tool to call with which arguments, and what to do when no spectackle MCP server is registered (the existing state template shows the pattern — follow it).
  Keep them short. These are one-line entry points, not tutorials.

GENERATION — the tool does the writing
  go build -o /tmp/spx-gen-cmd ./cmd/spectackle
  /tmp/spx-gen-cmd serve -root . -http 127.0.0.1:7403 &
then call the commands tool with op=gen and harness=[claude,copilot,codex]. A working client is at /tmp/claude-0/-home-user-spectackle/d0b8e016-f097-5792-857b-fd9ea4a8a781/scratchpad/mcp_http.py (base URL as argv[1], one JSON call per stdin line). With six new commands plus the two existing ones you should see substantially more ok gen lines than the five you get today — report the exact list. Kill the server afterwards.
Note AGENTS.md (codex) concatenates rather than emitting one file per command — read the generator to see how it handles multiple commands and make sure the new ones are included coherently rather than appended blindly.

TESTS (commands_test.go, extend what is there)
  1. every new template renders without error and is non-empty.
  2. rendered output contains no unresolved {{ }} — a template typo currently ships silently.
  3. the find template names only scopes that exist in scopeKinds; assert against the map itself so the test breaks when a scope is added or removed.
  4. every generated file carries the do-not-edit header.
  5. op=gen writes the expected file set for each harness — extend the existing assertion rather than adding a parallel one.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
Then read the generated .claude/commands/ files and confirm each new command is present and readable.

EXIT CRITERION
Six new templates rendering, all tests green under -race, ./... green, vet clean, lint clean, the generated surfaces regenerated BY THE TOOL and carrying every new command.

ROLLBACK
Six template files, their registration, test additions, and regenerated output. Deleting the templates and re-running the generator restores the prior file set exactly. No schema, stored format, record or anchor change.

REPORT BACK
The six template names, the exact ok gen list, each test's real output, how the codex/AGENTS.md concatenation handles the additions, and anything you deliberately did NOT do.
