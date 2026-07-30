# Cascading spec bundles

Specs are not monolithic. Rules live **next to the code they govern** in
`.spectackle/` folders and cascade like `.gitignore`/`CLAUDE.md`, so the
server can load exactly the rules for an impact radius — never the whole
corpus. See [lifecycle.md](lifecycle.md) for the full storage architecture.

## Locations & naming

```
.spectackle/spec.md            # root scope: repo-wide architecture rules + intent
<any-dir>/.spectackle/spec.md  # rules scoped to <any-dir>/ and below
```

Each context folder also holds `work.md` (active lifecycle items) and
`journal.ndjson` (history) — three bundle files max, no file sprawl.

## What the walk skips

Discovering bundles (`spec.Cascade.Load`, `workspace.Root.ContextDirs`, and
the coverage-gap walk behind `check`/`research`) never descends into:

- **A nested git boundary.** Any subdirectory below the workspace root that
  has its own `.git` entry — a directory (a nested/vendored clone) or a file
  holding a `gitdir: ...` pointer (a linked worktree or a submodule) — is a
  separate git checkout and is skipped wholesale. This is what makes agent
  worktrees invisible to the cascade regardless of which harness created
  them (Claude Code, Copilot, Codex, …, or a bare `git worktree add`): a
  linked worktree always carries a `.git` file, so there is nothing
  harness-specific to special-case.
- **Built-in defaults**: `.git`, `node_modules`, `testdata`, `bin`, `vendor`,
  `.spectackle` (spectackle's own state folder — never source, and handled
  specially anyway since it's what the walk is looking *for*).
- **`.spectackle/config.yaml` → `ignore`**: glob patterns matched against the
  repo-relative slash path of the directory, e.g. `generated/**`. Defaults to
  `[".git/**", "bin/**"]`.
- **`.spectackle/config.yaml` → `ignore_regex`**: RE2 patterns matched
  against the same repo-relative slash path, for shapes globs can't express,
  e.g. `["^vendor-[a-z]+$"]`. Empty by default. A malformed pattern is a
  config error at load time (`workspace: config.yaml: ignore_regex ...`).

All four sources are combined by `workspace.Root.SkipDir`, the single matcher
every walk shares — there is no per-tool or per-harness skip list to keep in
sync.

## spec.md format

Markdown, YAML front matter, flat `##` anchors — either a whitelisted prose
section (`intent`, `notes`, `design`, `context`) or one EARS rule per
rule-ID heading:

```markdown
---
schema: v0                # injected by the server; unknown stamp = regenerate
prefix: CUDA              # informational rule-ID prefix
scope: ["*.cu", "*.cuh"]  # globs relative to this dir; empty = everything below
inherits: true            # false cuts the cascade above this file
overrides: [ROOT-STY-001] # inherited rule IDs suppressed for this subtree
---

## intent
What this module is for; archive merges item outcomes here.

One line per record, enforced on write: `AppendIntent` skips a line whose
record ID the section already carries, and drops a pre-existing duplicate it
finds while it is there. Keyed on the ID rather than the whole line, because a
retried archive legitimately carries a different note and must not count as a
second statement — the first wins, since it records what landed rather than
the latest retry's phrasing. The heal is bounded to this section: an
ID-shaped bullet in a rule's rationale or in `notes`/`design`/`context` is
not an intent entry and is never touched.

## CUDA-KRN-001 {applies: cu:saxpy_kernel}
WHEN a kernel launch statement returns, the host wrapper SHALL check
cudaGetLastError and propagate its numeric value to the caller.

Rationale: launches fail asynchronously; an unchecked launch hides errors.
```

- **Rule ID**: `PREFIX-SEG-042` form, unique across the whole cascade (E006).
- **Sentence**: first non-blank paragraph line under the heading; must pass
  the EARS linter (docs/ears.md).
- **`Rationale:`** paragraphs are kept but not sent to the LLM by default.
- **`{applies: id,id}`** pins a rule to node IDs; the server anchors those
  bindings in `.spectackle/anchors.tsv` for drift detection.

## Resolution algorithm

For a file path `P` (`spec.Cascade.ForPath`):

1. Start at the root bundle, walk the directory spine down to `dir(P)`,
   shallow → deep, taking every bundle whose `scope` globs match `P`.
2. Deeper files **extend by default** (union). They win only explicitly:
   `overrides: [ID, …]` removes inherited rules; `inherits: false` drops
   everything inherited.
3. Result order = resolution order (root → leaf); each rule appears once.

## Authoring — server-managed, never hand-written

Contracts enter the cascade exclusively through the `rule` tool
(SPX-SPC-001): structured slots → deterministic composition → lint gate
(errors write nothing, SPX-SPC-002) → automatic ID (SPX-SPC-004) → scoped
bundle write with `schema: v0` injected. Missing slots are returned as
`need` records to the calling agent, which authors them (SPX-SPC-003,
ELICIT-001 — never a user form).
`rule op=edit` and `op=retire` complete the write surface; retired rule text
survives in the journal.

The files stay markdown-on-disk so humans review contracts in git diffs —
and `spectackle lint .` in CI guards against out-of-band hand edits (exit 1
on any E-severity finding).
