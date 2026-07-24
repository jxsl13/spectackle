# Cascading spec bundles

Specs are not monolithic. Rules live **next to the code they govern** in
`.spectacle/` folders and cascade like `.gitignore`/`CLAUDE.md`, so the
server can load exactly the rules for an impact radius — never the whole
corpus. See [lifecycle.md](lifecycle.md) for the full storage architecture.

## Locations & naming

```
.spectacle/spec.md            # root scope: repo-wide architecture rules + intent
<any-dir>/.spectacle/spec.md  # rules scoped to <any-dir>/ and below
```

Each context folder also holds `work.md` (active lifecycle items) and
`journal.ndjson` (history) — three bundle files max, no file sprawl.

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
  bindings in `.spectacle/anchors.tsv` for drift detection.

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
bundle write with `schema: v0` injected. Missing slots are elicited from the
end user via the MCP client or returned as `need` records (SPX-SPC-003).
`rule op=edit` and `op=retire` complete the write surface; retired rule text
survives in the journal.

The files stay markdown-on-disk so humans review contracts in git diffs —
and `spectacle lint .` in CI guards against out-of-band hand edits (exit 1
on any E-severity finding).
