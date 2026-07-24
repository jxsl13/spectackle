# MCP tool surface

Eleven orthogonal tools (eight lifecycle + three swarm). The Go structs in `internal/mcpserver/tools.go` are
the normative schema source (SPX-REPO-001 keeps this file consistent with
them). The server-description (MCP `instructions`, sent in the initialize
handshake) teaches the lifecycle loop — see `internal/mcpserver/server.go`.

## Design principles

- **Stable short IDs are the currency**: nodes `go:saxpy.Saxpy`, rules
  `CUDA-KRN-001`, items `P-0007`, sections `sec:gpu#intent`. The LLM names
  concepts, never file paths or contents.
- **Few tools, flat params, enums + defaults** (SPX-ARC-004): the common call
  is one or two fields; no nesting, no option floods.
- **Dense line records, not JSON, in results** (SPX-MCP-002).
- **Budgets and cursors** on read tools (SPX-ARC-002).
- **Corrections instead of errors** (SPX-ARC-003): unknown ID → `nf` with
  the nearest matches.
- **The LLM never writes .spectacle files** — `draft`, `rule`, `move`,
  `compact` are the only write paths, and they are server-side.

## Output line grammar

```
n <id> <kind> <file>:<line> [sig=<sig>]          node
e <src> <ekind> <dst> [via=<file>:<line>]        edge (call|incl|cgo|asm|launch|use|link)
r <ruleID> <P> <scopeDir> <text>                 rule (P: U|E|S|N|O|C)
r-root <ID> <ID> ...                             root-scoped rules, IDs only (full text via get)
i <id> <kind> <state> <dir> <title>              lifecycle item
s sec:<dir>#<name> <text>                        prose section
j <ref> <summary> :: <snippet>                   journal/history record
a <rule> <node> <file>:<s>-<e> <chash>           anchor
d <cls> <rule> <node> <file>:<s>-<e> [item=<id>] drift (gone|changed|stale)
g <kind> <ref> <msg>                             gap (uncovered|orphan)
c <dir> <reason> <n>                             compact candidate
! <code> <sev> <ref> <msg>                       finding (lint E001-E101, LEASE, WT, GATE, LOCK)
ag <name> <item|-> <hb-age>s <wt|main>           agent
l <path> <agent> <item|-> <exp>s                 scope lease
sw <seq> <agent> <ev> <ref|-> <msg>              swarm event (sibling learning, may prefix ANY result)
wt <item> <state> <root>                         worktree (open|gating|integrating|conflict|replaying)
need <slot> <question>                           missing input (elicitation fallback)
nf <id> <id> <id>                                not found — nearest matches
cur <token>                                      more results; pass back as cur
ok [<msg>]                                       success / nothing to report
#impact #contracts #rejections                   context-pack sections (draft)
#version #items #rules #graph #swarm #drift #health  snapshot sections (state)
```

## Tools

### 1. `find` — unified search (code + every lifecycle subcategory)

```json
{"type":"object","required":["q"],"properties":{
  "q":    {"type":"string"},
  "scope":{"enum":["code","rule","spec","proposal","task","bug","research","rejection","history","all"],"default":"all"},
  "k":    {"type":"integer","default":8}}}
```
`code`→graph, everything else→FTS5. **`rejection` and `history` are the
learn-before-planning scopes** — the loop starts here.

### 2. `get` — read one thing by ID

```json
{"type":"object","required":["id"],"properties":{
  "id":    {"type":"string","description":"node|rule|item|sec:<dir>#<name>|path"},
  "depth": {"type":"integer","default":0},
  "budget":{"type":"integer","default":2000},
  "cur":   {"type":"string"}}}
```
Dispatch on ID shape: item→header+body; rule→text+rationale+`a` anchors;
node with `depth>0`→cross-language impact radius (`n`/`e`, BFS); dir→scoped
rules+items; file→resolved contracts; unknown→`nf`.

### 3. `draft` — create a lifecycle item (state=draft)

```json
{"type":"object","required":["kind","title"],"properties":{
  "kind":   {"enum":["proposal","task","research","bug"]},
  "title":  {"type":"string"},
  "body":   {"type":"string"},
  "targets":{"type":"array","items":{"type":"string"}},
  "parent": {"type":"string"},
  "dir":    {"type":"string"}}}
```
Server assigns ID (`P-0001`…) and context dir (targets→deepest common
context, else root). With `targets` the response is the **context pack**:
`#impact` (radius), `#contracts` (binding EARS rules), `#rejections`
(similar past failures) — the synergy moment, one round trip. Root-scoped
rules collapse into a single `r-root` ID-only line instead of repeating
their full text every draft, and any of the three sections with nothing to
report is omitted outright rather than filled with an `ok` placeholder
(SPX-MCP-004).

### 4. `rule` — author EARS contracts (the only rule write path)

```json
{"type":"object","required":["op"],"properties":{
  "op":       {"enum":["add","edit","retire"]},
  "id":       {"type":"string"},
  "dir":      {"type":"string"},
  "pattern":  {"enum":["U","E","S","N","O","C"]},
  "system":   {"type":"string"}, "response":{"type":"string"},
  "trigger":  {"type":"string"}, "state":   {"type":"string"},
  "condition":{"type":"string"}, "feature": {"type":"string"},
  "stem":     {"type":"string"}, "rationale":{"type":"string"},
  "applies":  {"type":"array","items":{"type":"string"}},
  "item":     {"type":"string"}}}
```
`add`: slots → server composes the canonical sentence, lints (errors reject,
nothing written — SPX-SPC-002), auto-IDs (SPX-SPC-004), appends to the scoped
spec.md, journals, anchors `applies` for drift. Missing slots are **elicited
from the end user** (MCP elicitation form) or returned as `need` records.
`edit`: recompose/relink by id. `retire`: removed from spec.md; full text
survives in the journal.

### 5. `move` — lifecycle transition

```json
{"type":"object","required":["id","to"],"properties":{
  "id":  {"type":"string"},
  "to":  {"enum":["submitted","approved","rejected","active","done","archived"]},
  "note":{"type":"string"}}}
```
States are totally ordered (`draft < submitted < approved < active < done <
archived`); **any forward skip is legal in one call** — `draft` straight to
`active`, or `approved` straight to `archived`, cost one `move` each, not a
walk through every state in between. `rejected` is reachable from any state
except `archived`, REQUIRES `note` (the rejection corpus), and is
**revocable**: move the rejected ID back to `draft`/`submitted`/`approved`/
`active` (never `done`/`archived`) — the reject event snapshots the full
item. `done → active` (reopen) is the one backward hop outside rejection.
`archived` requires no open children; a skip straight to `archived` implies
`done` and runs the archive effects once — merges the delta into spec.md
`## intent`. `archived` is terminal. Illegal transition → `!` with the
allowed set. Approve/reject only on explicit user instruction.

### 6. `check` — verify (drift, coverage, lint, compact-due)

```json
{"type":"object","properties":{
  "path":  {"type":"string"},
  "fix":   {"type":"boolean","default":false},
  "budget":{"type":"integer","default":1500}}}
```
Emits `!` lint findings, `g` coverage gaps, `d` drift records (anchor
classification; position-only moves are silently refreshed), `E101`
duplicate item IDs (branch-merge backstop), `c` compact-due signals.
`fix=true` drafts one backprop proposal per drifted rule and re-stamps
anchors. Run until `ok` before `move to=done`.

### 7. `compact` — housekeeping (dry-run by default)

```json
{"type":"object","properties":{
  "path": {"type":"string"},
  "apply":{"type":"boolean","default":false}}}
```
Candidates: done-unarchived items (apply archives them), journal folds over
`journal_max`. Folds drop `create/move/rule/drift` noise; **`reject`,
`archive` and `compact` events are never dropped**.

### 8. `lease` — scope reservations (multi-agent)

```json
{"type":"object","required":["op"],"properties":{
  "op":   {"enum":["claim","release","ls"]},
  "paths":{"type":"array","items":{"type":"string"},"description":"dirs/files or item IDs"},
  "item": {"type":"string"},
  "ttl":  {"type":"integer","description":"seconds, default 600"}}}
```
Prefix-overlap of a live foreign lease → `! LEASE E` + `l` line naming the
holder (SPX-SWM-003). Own leases auto-refresh on every tool call; release
explicit claims the moment the item is done — a stale claim blocks siblings
until TTL expiry. Stale agents (no heartbeat > `agent_ttl`) are swept:
their leases expire and their registry rows are removed (SPX-SWM-006); a
clean shutdown deregisters immediately. `work op=start` auto-claims its
item + targets.

### 9. `work` — git-worktree lifecycle (multi-agent isolation)

```json
{"type":"object","required":["op"],"properties":{
  "op":  {"enum":["start","submit","abort","status"]},
  "item":{"type":"string","description":"required for start; defaults to own active item"}}}
```
`start`: lease scope, create worktree + branch `spectacle/<item>` under
`.spectacle/wt/`, mirror live spec state in, re-root the session — the `wt`
line names YOUR edit/build root; spectacle paths stay repo-relative.
`submit`: gate (config `verify:` + item `goal:`) → **code-only commit**
(`.spectacle` excluded — SPX-SWM-001) → merge main into the branch →
re-gate → `--ff-only` to main → **semantic replay** of the .spectacle delta
→ teardown. Conflicts come back as `! WT E conflict <files>` — resolve in
the worktree, submit again (resumable). `abort`: teardown, item back to
approved.

### 10. `swarm` — sibling awareness (zero params)

```json
{"type":"object","properties":{}}
```
→ `ag` agents, `l` leases, `wt` open worktrees, `sw` recent learnings.
Unseen `sw` events are additionally prepended to every tool result
(realtime piggyback); `find scope=rejection` unions live sibling rejections
before they ever merge (SPX-SWM-002).

### 11. `state` — one read-only structured snapshot

```json
{"type":"object","properties":{
  "path":  {"type":"string","description":"subtree, default all"},
  "budget":{"type":"integer","default":2000}}}
```
The full spec-driven-development picture in one call, strictly read-only —
unlike `check`, it writes nothing (no `drift.Save`, no backprop drafts, no
journal, no anchor re-stamp). Sections, each omitted entirely when it has
nothing to report (SPX-MCP-004 spirit): `#version` (server version, agent
name, active root), `#items` (counts by state + `i` lines, scoped to
`path`), `#rules` (per-context-dir rule counts + a global lint-findings
count), `#graph` (`g.Stats()` node/edge totals), `#swarm` (`ag`/`l`/`wt`
lines), `#drift` (anchor classification summary + `d` lines for
changed/gone/stale — `moved` anchors are counted, never silently
re-stamped), `#health` (compact-due `c` lines + a coverage-gap count).
Budget-truncated like every other read tool (SPX-ARC-002). Same content is
exposed as the `state` MCP prompt (`internal/mcpserver/prompts.go`) via the
shared `(s *Server) stateText(path string)` builder.

### 12. Prompts — slash-command entry points

Three MCP prompts (`prompts/get`, no arguments unless noted) in
`internal/mcpserver/prompts.go`, registered by `(s *Server) registerPrompts()`
(not yet wired into `New` — the orchestrator calls it). They bypass `gate()`
(prompts/get is not a tool call): each handler locks `s.mu` and calls
`s.scan.Refresh()` itself so the snapshot below is current.

**`workflow`** (no args) — a standing situational-awareness dump: line 1
`spectacle workflow - state below is live`, then `AGENTS/LEASES` (`ag`/`l`
lines from `s.cd.Agents()`/`s.cd.Leases()`), `ACTIVE ITEMS` (`i` lines from
`item.LoadAll`, non-draft items surfaced first), and `LOOP` — the six-step
lifecycle checklist condensed from the server's `instructions` manifest.

**`next`** (optional `item` string arg) — the full implementer brief for one
item: with `item` given, that item (or `nf <id>` if unknown); otherwise the
first `approved` item, `kind=task` preferred, falling back to any approved
item, or `ok nothing approved - draft or approve first` if none exists. Body
is `item.Record` + `parent`/`targets`/body verbatim, followed by the 5-step
IMPLEMENTER PROTOCOL (`get` → `lease op=claim` → `move to=active` →
implement+test → `move to=done` + `lease op=release`), using the item's
context dir as the suggested lease path.

**`state`** (optional `path` string arg) — identical content to the `state`
tool (`tools/call state`), reached as a slash command
(`/mcp__spectacle__state`) instead of a tool call.

## Fold map (previous 11-tool surface → 7 lifecycle tools)

`sym`→`find scope=code` · `map`→`get <dir>` · `impact`→`get depth` ·
`contracts`/`plan_change`→`draft` context pack + `get` · `lint_ears`+
`coverage`→`check` · `link`→`rule applies` · `add_rule`/`rm_rule`→`rule` ·
`reindex`→automatic sync (CLI `spectacle reindex` for debugging).

## Status

Everything above is live except graph-backed parts (`find scope=code`,
`get depth`, `#impact`, anchor spans), which return documented
empty/pending results until the M1 indexer fills the graph.
