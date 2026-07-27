# The spec lifecycle — architecture

spectackle is the single source of truth, sole orchestrator and abstraction
layer for spec-driven development. The LLM never creates or edits lifecycle
files — everything runs through structured tool calls; the server owns the
files. This document is the blueprint for that lifecycle.

## 1. Git-native storage & file abstraction

### 1.1 Layout — everything lives in `.spectackle/` folders

```
<workspace root>/
  .spectackle/                    # ROOT folder (marker: contains config.yaml)
    config.yaml                  # settings + compact thresholds (schema: v0)
    spec.md                      # living spec, root scope: intent + EARS rules
    work.md                      # ACTIVE lifecycle items (server-managed)
    journal.ndjson               # append-only history (transitions, rejections)
    anchors.tsv                  # rule↔node↔span-hash bindings (workspace-wide)
    .gitignore                   # server-written: "cache/"
    .gitattributes               # server-written: "journal.ndjson merge=union"
    cache/index.db               # NOT versioned (SQLite FTS5, pure Go)
  <any-dir>/.spectackle/          # nested context folder
    spec.md · work.md · journal.ndjson · .gitattributes
```

Every server write is confined to a `.spectackle/` folder (SPX-ARC-005); the
rest of the workspace is never touched by lifecycle writes. Everything in
those folders **except `cache/`** is versioned — the knowledge base travels
with the repo, reviews happen in git diffs, branches merge it like code.

### 1.2 Anti file-sprawl: bundles, not files

OpenSpec-style per-item files burn tokens (directory listings, tiny reads)
and clutter reviews. spectackle bundles by role — a context folder holds at
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
**`.spectackle/config.yaml`** (the folder alone is ambiguous — nested context
dirs have `.spectackle/` folders too), falls back to the `.git` root, then to
the `-root` flag. Context mapping for a new item: explicit `dir` param >
deepest common directory of the `targets`, snapped to the nearest existing
context dir > root. Scaffolding (`.gitignore`, `.gitattributes`,
`config.yaml`, frontmatter) is created lazily and server-side. A **newly**
created `config.yaml` is fully self-documenting: every setting (`schema`,
`langs`, `ignore`, `ignore_regex`, `budget_default`, `compact.journal_max`,
`compact.done_max`, `swarm.lease_ttl`, `swarm.agent_ttl`,
`feedback.max_rounds`, `feedback.grill`, `worktrees_dir`, `coverage_gate`) is
written with its
default value and a short trailing comment, so the file doubles as reference
docs and an editable template (`workspace.scaffoldConfigYAML`,
`internal/workspace/workspace.go`). An **existing** `config.yaml` is never
regenerated or rewritten by this or any later scaffold call — hand edits are
permanent until the user changes them.

### 1.4 Full abstraction

The LLM interacts with **semantic concepts only**: items (`P-0007`), rules
(`CUDA-KRN-001`), nodes (`go:saxpy.Saxpy`), sections (`sec:gpu#intent`).
Which file a concept lives in, how blocks are anchored, where frontmatter
goes — invisible. Every server-written file carries `schema: v0` in its
frontmatter; an unknown stamp is a tool error ("regenerate"). **There is no
schema migration** — pre-1.0, formats may break freely; the stamp rotates and
the cache rebuilds.

### 1.5 ID forms on git surfaces

Human-facing git surfaces — branch names (`spectackle/<short>`), pull
request titles, commit subjects — render the SHORT display ID (kind prefix
plus the first `ids.MinRecordPrefixLen` body characters, which pin all 48
timestamp bits and stay unambiguous for the repository lifetime). Machine
surfaces keep the FULL ID: the `Spectackle-Item`/`Spectackle-Eid` commit
trailers are the journal-to-git audit join, the PR body's first line
carries it for exact search, and every record inside `.spectackle/` files
is full-length (inside git-managed files the form does not matter for
humans, so the resolvable full form wins). Branches created before this
convention keep their full-length names; the flow falls back to them.

### 1.6 Git modes: offline by default

`git.mode` defaults to **offline** (GIT-DEFAULT-001): every lifecycle edge
commits code and records on the CURRENT branch — `g offline commit
<short-sha> <subject>` — and creates **no branches, no pull requests, no
pushes, no base checkouts**. Online operation (branch per item, draft PR
at activation, ready at done, merge at archive) is the explicit opt-in
`git: mode: online`; repositories that relied on the old implicit online
default must add that key. The swarm worktree flow is mode-exempt by
recorded decision (ADR): its transient local branches are already push-
and forge-free and behave identically in both modes.

Accepted offline losses, stated not hidden: closure records land on
whatever branch is current — nothing merges them to the base branch, so
records archived on a later-deleted side branch are gone with it; a
detached-head worktree submit is no longer self-healing at archive (the
submit result says to check out a branch and merge); the post-merge
orchestrator sync ritual (wait for the merged line, then re-sync) is
online-only — offline there is no merged line to wait for. Legacy offline
workspaces may hold parked `spectackle/<id>` branches and a
`cache/forge-offline.json` from the old PR-simulation era; both are inert.

## 2. Unified high-performance search (the persisted cache)

One SQLite file (`.spectackle/cache/index.db`, `modernc.org/sqlite` — pure Go,
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
- validation gates `done → archived` for tasks and bugs: the `validate`
  tool renders a computed pack over the item's real diff and an independent
  verdict (`validate op=verdict`, a second deliberate `SPECTACKLE_AGENT`,
  never the implementer) must pass with a current ATTRIBUTED-diff hash
  (the branch merge plus commits citing the item — uncited commits are
  invisible to attribution, a stated residual) —
  `feedback.validate: require` hard-refuses, the default warns — except
  when the LANDED diff trips a risk input (distinct file count at or over
  `feedback.risk_files`, default 8, or a file inside a
  `feedback.dangerous_paths` glob, default empty): then warn mode still
  requires the verdict and the refusal names the tripped input. Risk is
  computed from the attributed diff, never from declared targets (which
  are gameable); an explicit `require` is never downgraded. A failing
  verdict reopens `done → active` through the existing hop with the
  findings as the implementer's next brief; each reopen counts a round and
  exhaustion escalates to `blocked` exactly as before;
- research items archive only consumed (a live or archived item citing
  them, or a rule rationale naming them) or explicitly closed with a
  substantive note — hard, no loose mode: research that changes nothing
  and says nothing is pure token cost (`! BACKPROP E`);
- `archived` requires no open children (proposals: no open child items); a
  skip straight to `archived` (e.g. from `active`) **implies `done`** and
  runs the archive effects exactly once — merges the outcome into `##
  intent`, journals a summary, folds done children, removes the blocks from
  work.md;
- `archived` is terminal — no further transitions.

### Architecture decisions (ADRs)

Architecture decisions live in the lifecycle as first-class `adr` items (ID letter `ADR`, e.g. `ADR-0042`), queryable via `find scope=adr` and drift-anchored like any other record. Each ADR captures four structured fields following the classic ADR template: **Context** (the forces and constraints behind the decision), **Decision** (the chosen option), **Consequences** (trade-offs and follow-on effects), and **Status** (proposed/accepted/superseded/deprecated — the status field survives from decision proposal through acceptance and eventual supersession). ADRs are never loose markdown — they are persisted as lifecycle items in work.md, journaled on every state transition, and fully subject to the lifecycle's audit trail and search. This keeps architecture rationale discoverable, versioned with the code, and part of the shared knowledge base for all agents in the swarm.

### The pre-push hook: one local gate, both kinds of pushes

The workspace `verify` commands gate automated `done` transitions; a human
push bypasses them and burns a runner on code that may not build. `state`'s
`#health` renders `w hook pre-push absent` when verify commands are
configured but no hook runs them; the opt-in `spectackle hook install`
writes a one-line hook shelling to `spectackle verify` — the same commands
from the same config, one gate definition. The server never writes into
`.git` uninvited, and a foreign pre-push hook is never clobbered
(recommendation over imposition, ADR-01KYDG's said-not-slipped-in).

### Nodes are judgment, edges are mechanics (NODE-EDGE-001)

Every lifecycle STATE is an LLM interaction: the caller reads the hints
(`nextAction`, the packs) and decides direction and work — implement,
document, review, test, decide — with the legal alternatives rendered as
an or-tail, never a single rail. Every TRANSITION is a fully mechanical
server step (branch, commit, push, PR, merge, sweep — `gitFlowFor`), and
no tool result may demand mechanical work of the caller. The one honest
exception is a resident without the self-restart watcher: a dying binary
cannot replace itself, so the stale hint names `make dev` as the
operator's step — the caller's judgment there is only whether to trust a
stale surface.

### Compacting — hybrid, and why

1. **Event-driven at archive** (primary): archive is the only moment with
   complete item semantics — the server knows which delta to merge, which
   children to fold, what summary to journal. One git-visible change at a
   natural review boundary.
2. **Threshold-driven via `check`** (safety net): dirs where nothing archives
   still accrue noise. `check` (already in the loop) emits `c` records when
   `journal_max`/`done_max` (`config.yaml`, `compact.journal_max` defaults to
   **300** events since the last compact, `compact.done_max` to 8) trip; the
   LLM then runs `compact` (dry-run → `apply=true`). Journal folds drop
   `create/move/rule/drift` noise; `reject`/`archive`/`compact`/
   `escalate`/`decide` lines are kept verbatim, and verdict events
   (`review`, `validate`) survive too (ADR-01KYES0TT): identity, hash,
   pass and lenses forever — the per-key addressal detail (`keys`/`wv`)
   is pruned once the item is archived or rejected, so retention never
   bloats the journals compaction exists to shrink. Archived items'
   grill/validate packs render `computed: suppressed (archived)` instead
   of re-critiquing a tree that has moved on.
   - The server also surfaces this proactively, without waiting for an
     explicit `check`: once the root journal crosses `journal_max`, EVERY
     tool result carries a `c . journal <n> events since last compact` line
     (`postCall`, `internal/mcpserver/swarm.go`) — the same record shape
     `check` itself would emit, just piggybacked like the `sw` sibling-
     learning lines. The hint fires **once per crossing**: it stays silent
     on later calls until either a `compact` runs (the count drops back
     below the threshold) or the journal grows by another full
     `journal_max` without one. The underlying count is cached and only
     re-read from disk at most once every 30s, so the nudge costs nothing on
     the common call.
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

**Bindings**: `.spectackle/anchors.tsv` (versioned, root-only) rows
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
spectackle stdio process (works with every flat-rate agent tool; no daemon),
coordinated through a shared WAL-SQLite `coord.db` in the MAIN repo's
`.spectackle/cache/` (a linked worktree resolves its parent via
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
`.spectackle/wt/<item>/` (NOT `cache/` — cache is disposable, in-flight work
is not) on branch `spectackle/<item>`. The session re-roots into the
worktree; live .spectackle state is mirrored in at start. Submit pipeline:
gate (config `verify:` + item `goal:`) → commit **code only** (pathspec
excludes every `.spectackle` dir — SPX-SWM-001) → merge main INTO the branch
→ re-gate the merged tree → `--ff-only` into main (under the integrate
lock this cannot conflict) → **semantic replay**.

**Conflict-free .spectackle merging**: git never textually merges spec
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
`rule`, `reindex`→automatic sync (CLI `spectackle reindex` remains for
debugging).
