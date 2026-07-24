# The spec lifecycle — architecture

spectacle is the single source of truth, sole orchestrator and abstraction
layer for spec-driven development. The LLM never creates or edits lifecycle
files — everything runs through structured tool calls; the server owns the
files. This document is the blueprint for that lifecycle.

## 1. Git-native storage & file abstraction

### 1.1 Layout — everything lives in `.spectacle/` folders

```
<workspace root>/
  .spectacle/                    # ROOT folder (marker: contains config.yaml)
    config.yaml                  # settings + compact thresholds (schema: v0)
    spec.md                      # living spec, root scope: intent + EARS rules
    work.md                      # ACTIVE lifecycle items (server-managed)
    journal.ndjson               # append-only history (transitions, rejections)
    anchors.tsv                  # rule↔node↔span-hash bindings (workspace-wide)
    .gitignore                   # server-written: "cache/"
    .gitattributes               # server-written: "journal.ndjson merge=union"
    cache/index.db               # NOT versioned (SQLite FTS5, pure Go)
  <any-dir>/.spectacle/          # nested context folder
    spec.md · work.md · journal.ndjson · .gitattributes
```

Every server write is confined to a `.spectacle/` folder (SPX-ARC-005); the
rest of the workspace is never touched by lifecycle writes. Everything in
those folders **except `cache/`** is versioned — the knowledge base travels
with the repo, reviews happen in git diffs, branches merge it like code.

### 1.2 Anti file-sprawl: bundles, not files

OpenSpec-style per-item files burn tokens (directory listings, tiny reads)
and clutter reviews. spectacle bundles by role — a context folder holds at
most **three content files**, regardless of item or rule count:

- **`spec.md`** — the living spec: `## intent` (+ optional `notes`, `design`,
  `context`) prose sections and one `## <RULE-ID>` block per EARS rule.
- **`work.md`** — active items only, one `## <ID> <title>` block each with a
  flat `key: value` machine header (kind, state, created, parent, targets,
  rules) and free prose body. Rejected/archived items *leave* this file.
- **`journal.ndjson`** — append-only event log, one compact JSON object per
  line (`create/move/rule/archive/reject/drift/compact`). Server-written
  `.gitattributes` sets `merge=union`: append-only + union merge = the
  highest-churn file merges conflict-free across branches.

Item blocks keep merge conflicts block-local; journals keep history out of
the reviewable files; the archive/reject transitions keep work.md bounded.

### 1.3 Workspace discovery & context mapping

`workspace.Detect` walks up from the start dir looking for
**`.spectacle/config.yaml`** (the folder alone is ambiguous — nested context
dirs have `.spectacle/` folders too), falls back to the `.git` root, then to
the `-root` flag. Context mapping for a new item: explicit `dir` param >
deepest common directory of the `targets`, snapped to the nearest existing
context dir > root. Scaffolding (`.gitignore`, `.gitattributes`,
`config.yaml`, frontmatter) is created lazily and server-side.

### 1.4 Full abstraction

The LLM interacts with **semantic concepts only**: items (`P-0007`), rules
(`CUDA-KRN-001`), nodes (`go:saxpy.Saxpy`), sections (`sec:gpu#intent`).
Which file a concept lives in, how blocks are anchored, where frontmatter
goes — invisible. Every server-written file carries `schema: v0` in its
frontmatter; an unknown stamp is a tool error ("regenerate"). **There is no
schema migration** — pre-1.0, formats may break freely; the stamp rotates and
the cache rebuilds.

## 2. Unified high-performance search (the persisted cache)

One SQLite file (`.spectacle/cache/index.db`, `modernc.org/sqlite` — pure Go,
FTS5 verified, `CGO_ENABLED=0` holds):

| table | content |
|---|---|
| `meta` | generation stamp; mismatch ⇒ drop + rebuild (no migrations) |
| `files` | per-bundle-file mtime+size for the fast path |
| `docs` (FTS5) | `kind, id, dir, title, body` — every searchable record |

Doc kinds: `rule`, `section`, `proposal`, `task`, `bug`, `research`,
`journal`, `rejection`. Reject events are indexed under their own kind so
`find scope=rejection` is a pure kind filter. The M1 indexer adds
`nodes/edges/blobs` tables; `find scope=code` then queries the graph.

**Sync**: the versioned files are the source of truth; the cache is
disposable. A debounced (300 ms) `Refresh` gates every tool call: stat scan
(mtime+size) per bundle file, changed files are re-parsed and their doc kinds
replaced (FTS5 has no PK — delete-by-`(dir, kinds)` + reinsert). Server-side
writes void the debounce (`MarkDirty`), so effects are visible to the very
next call. FTS queries are sanitized (quoted tokens, OR-joined) — the LLM
never passes raw MATCH syntax.

## 3. The lifecycle (Cavekit × SpecKit × OpenSpec)

What was fused: **SpecKit** — intent (proposal) is separated from work
(tasks, linked via `parent`). **OpenSpec** — a proposal carries its
delta-spec in the body; **archiving merges the delta** into the living
spec.md. **Cavekit v4** — a tight, self-checking loop with dense records; but
plain language everywhere, *no* caveman encoding.

```
            find(rejection|history) ──── learn before planning
                     │
     draft(proposal, targets) ──→ CONTEXT PACK (#impact #contracts #rejections)
                     │
   user approval → move(approved) → draft(tasks) + rule(add) contracts
                     │
              implement code
                     │
        check ──d records──→ rule(edit) | code fix   (loop until ok)
                     │
      move(done) → move(archived) ──→ delta merged into spec.md ## intent
                     │
              compact when check says c
```

States: `draft → submitted → approved → active → done → archived`, plus
`rejected`. Transitions follow a total order over those six states
(`draft`(0) < `submitted`(1) < `approved`(2) < `active`(3) < `done`(4) <
`archived`(5)): **any forward jump is legal in a single `move` call** — every
hop is optional, so `draft → active` or `approved → archived` costs one
tool call, not a walk through every intermediate state. Server-enforced
guards:
- `rejected` is reachable from any of the six states **except `archived`**,
  and **requires a note** — that note is the searchable corpus that prevents
  rework;
- rejections are **revocable**: the reject journal event snapshots the full
  item (body, targets, parent, rules), so `move` can restore a rejected ID
  into `draft`, `submitted`, `approved` or `active` (never `done`/`archived`)
  — and reject events survive every compaction;
- `done → active` (reopen) is the one backward hop kept outside rejection;
- `archived` requires no open children (proposals: no open child items); a
  skip straight to `archived` (e.g. from `active`) **implies `done`** and
  runs the archive effects exactly once — merges the outcome into `##
  intent`, journals a summary, folds done children, removes the blocks from
  work.md;
- `archived` is terminal — no further transitions.

### Compacting — hybrid, and why

1. **Event-driven at archive** (primary): archive is the only moment with
   complete item semantics — the server knows which delta to merge, which
   children to fold, what summary to journal. One git-visible change at a
   natural review boundary.
2. **Threshold-driven via `check`** (safety net): dirs where nothing archives
   still accrue noise. `check` (already in the loop) emits `c` records when
   `journal_max`/`done_max` (config.yaml) trip; the LLM then runs `compact`
   (dry-run → `apply=true`). Journal folds drop `create/move/rule/drift`
   noise; `reject`/`archive`/`compact` lines are kept verbatim.
3. **Continuous compaction: rejected.** It would mutate versioned files on
   read paths (git diff noise, destroyed review ergonomics), defeat
   `merge=union` by rewriting journals mid-branch, and compact without
   completion semantics.

### Self-bootstrapping

The MCP server-description (`ServerOptions.Instructions`, part of the
initialize handshake) teaches the entire loop — see the verbatim string in
`internal/mcpserver/server.go`. A fresh LLM session needs no other document
to operate the lifecycle correctly.

## 4. Drift detection & backpropagation

**Bindings**: `.spectacle/anchors.tsv` (versioned, root-only) rows
`rule ⇥ node ⇥ file ⇥ span ⇥ chash ⇥ rhash`. `chash` = 16-hex sha256 over the
**normalized** definition span (CRLF→LF, per-line trailing whitespace
stripped, outer blank lines dropped — indentation preserved, it is semantic
in asm). Hashing content instead of positions makes pure line shifts
drift-free. Anchors are written by `rule op=add|edit` with `applies`, and
re-stamped by `check`.

**Classification** per anchor on `check`: rule missing ⇒ `stale`; node
missing ⇒ `gone`; hash differs ⇒ `changed`; same hash, new position ⇒
silent refresh; graph empty (pre-M1) ⇒ `pending`, never a false alarm.

**Backpropagation**: `check fix=true` drafts one **backprop proposal** per
drifted rule into the scope's work.md — body carries rule text, node,
old/new hashes and the two legal resolutions: `rule op=edit` (spec follows
code) or revert (code follows spec). A `drift` journal event links rule,
node, hashes and the item. The LLM decides; the server never silently
rewrites a contract.

## 5. Multi-agent swarm: leases, worktrees, replay merging

Multiple autonomous agents operate on one repo in parallel — each its own
spectacle stdio process (works with every flat-rate agent tool; no daemon),
coordinated through a shared WAL-SQLite `coord.db` in the MAIN repo's
`.spectacle/cache/` (a linked worktree resolves its parent via
`git rev-parse --git-common-dir`).

**coord.db owns** (ephemeral coordination, not knowledge): the agent
registry (heartbeat per tool call; stale agents auto-expire), scope
**leases** (path prefixes + item IDs; overlap of a live foreign lease
rejects a claim with the holder named), the **global ID counters** (item and
rule IDs — floor-seeded `max(stored, file-scan)+1`, so deleting the cache
never regresses IDs and two worktrees can never mint the same ID,
SPX-SWM-004), the **swarm event log** (dual-written rejections/rules/drift —
`find scope=rejection` unions it, and unseen events piggyback as `sw` lines
on every tool result: agent B learns of agent A's failed hypothesis before
it ever merges, SPX-SWM-002), replay bookkeeping and the single
**integrate lock**.

**Worktree lifecycle** (`work start/submit/abort`): worktrees live under
`.spectacle/wt/<item>/` (NOT `cache/` — cache is disposable, in-flight work
is not) on branch `spectacle/<item>`. The session re-roots into the
worktree; live .spectacle state is mirrored in at start. Submit pipeline:
gate (config `verify:` + item `goal:`) → commit **code only** (pathspec
excludes every `.spectacle` dir — SPX-SWM-001) → merge main INTO the branch
→ re-gate the merged tree → `--ff-only` into main (under the integrate
lock this cannot conflict) → **semantic replay**.

**Conflict-free .spectacle merging**: git never textually merges spec
bundles. Journal events are the operation log (CRDT-style, each with a
unique `eid`); at submit the worktree's event delta (events absent from
main's live journal and the applied-set) replays onto main through the same
code paths the tools use — journal appends verbatim, rule ops (colliding
IDs re-minted with a `remap` notice), work.md reconciled to final state,
intent lines containment-checked, anchors re-stamped. Every step is
idempotent, so a submit retried after a partial failure resumes precisely.
`compact` is blocked inside worktrees (SPX-SWM-005) — folds would corrupt
the delta.

## 6. Tool surface

Seven orthogonal tools — `find, get, draft, rule, move, check, compact` —
with flat parameters; exact JSON Schemas in [tools.md](tools.md). Folds from
the previous 11-tool surface: `sym`→`find scope=code`, `map`→`get <dir>`,
`impact`+`contracts`+`plan_change`→`get depth` / `draft` context pack,
`lint_ears`+`coverage`→`check`, `link`→`rule applies`, `add_rule`/`rm_rule`→
`rule`, `reindex`→automatic sync (CLI `spectacle reindex` remains for
debugging).
