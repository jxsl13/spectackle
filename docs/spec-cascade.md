# Cascading spec files

Specs are not monolithic. Rules live **next to the code they govern** and
cascade like `.gitignore`/`CLAUDE.md`, so the server can load exactly the
rules for an impact radius — never the whole corpus.

## Locations & naming

```
.spectacle/
├── config.yaml          # langs, ignore globs, cache path, budget default
├── global.ears.md       # repo-wide architecture rules (always resolved first)
├── links.tsv            # tool-written rule↔node traceability (committed)
└── cache.db             # parse cache (gitignored)

<any-dir>/.spectacle.ears.md   # rules scoped to <any-dir>/ and below
```

Example project layout:

```
repo/
├── .spectacle/global.ears.md        # GLB-…   architecture-wide
├── .spectacle.ears.md               # ROOT-…  repo conventions
├── asm_math/.spectacle.ears.md      # ASM-…   Plan 9 asm rules
└── cuda_kernels/.spectacle.ears.md  # CUDA-…  kernel rules
```

## File format

Markdown with YAML front matter; one rule per `##` heading:

```markdown
---
prefix: CUDA            # informational rule-ID prefix
scope: ["*.cu", "*.cuh"]  # globs relative to this dir; empty = everything below
inherits: true          # false cuts the cascade above this file
overrides: [ROOT-STY-001] # inherited rule IDs suppressed for this subtree
---

## CUDA-KRN-001 {applies: cu:saxpy_kernel}
WHEN a kernel launch statement returns, the host wrapper SHALL check
cudaGetLastError and propagate its numeric value to the caller.

Rationale: launches fail asynchronously; an unchecked launch hides errors.
```

- **Rule ID**: `PREFIX-SEG-042` form (`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-\d{3}$`),
  unique across the whole cascade (E006).
- **Sentence**: first non-blank paragraph line under the heading; must pass
  the EARS linter (docs/ears.md).
- **`Rationale:`** paragraphs are kept but never sent to the LLM by default.
- **`{applies: id,id}`** optionally pins a rule to specific node IDs;
  `.spectacle/links.tsv` (written by the `link` tool) is the dynamic
  equivalent.

## Resolution algorithm

For a file path `P` (as computed by `spec.Cascade.ForPath`):

1. Start with `.spectacle/global.ears.md`.
2. Append every `.spectacle.ears.md` on the directory spine from repo root
   down to `dir(P)`, shallow → deep, **if** its `scope` globs match `P`
   (globs are relative to the spec file's directory; `**/` matches any
   depth including zero).
3. Deeper files **extend by default** (union). They win only explicitly:
   - `overrides: [ID, …]` removes inherited rules by ID;
   - `inherits: false` drops *everything* inherited (including global).
4. Result order = resolution order (global → leaf); each rule appears once.

Worked example (from the test suite): for `gpu/kern.cu` with
`gpu/.spectacle.ears.md` declaring `overrides: [ROOT-STY-001]`, the resolved
set is `GLB-ARC-001, GPU-KRN-001` — the root style rule is gone, the global
architecture rule is kept.

## Authoring — server-managed, never hand-written

Spec files are **artifacts of the MCP server**, not hand-edited documents
(SPX-SPC-001). Contracts enter the cascade through the `add_rule` tool:

1. The caller (agent or end user) supplies **structured slots** — pattern,
   system, response, and the pattern's condition clause — never a free-text
   sentence. Missing slots are **elicited from the end user** through the MCP
   client (elicitation form) when the client supports it; otherwise the tool
   returns `need <slot> <question>` records for the agent to relay.
2. The server **composes** the canonical EARS sentence deterministically
   from the slots and **lints** it; any error-severity finding rejects the
   call before a single byte is written (SPX-SPC-002).
3. The server assigns the **next free ID** for the stem across the whole
   cascade (SPX-SPC-004) and appends the rule to the target
   `.spectacle.ears.md`, creating the file with front matter if needed.

`rm_rule` is the inverse. The files stay markdown-on-disk so humans review
contracts in git diffs — and `spectacle lint .` in CI guards against
out-of-band hand edits (exit 1 on any E-severity finding, E006 catches
duplicate IDs).

Style guidance still applies to slot content: keep global rules few and
structural, push specifics down into scoped files, prefer a new scoped file
over widening an existing one.
