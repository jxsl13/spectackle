# MCP tool surface

Ten orthogonal tools (seven lifecycle + three swarm). The Go structs in `internal/mcpserver/tools.go` are
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
(similar past failures) — the synergy moment, one round trip.

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
`rejected` REQUIRES `note` (the rejection corpus) and is **revocable**: move
the rejected ID back to any previous state — the reject event snapshots the
full item. `archived` requires `done` + no open children; merges the delta
into spec.md `## intent`. Illegal transition → `!` with the allowed set.
Approve/reject only on explicit user instruction.

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
holder (SPX-SWM-003). Own leases auto-refresh on every tool call; stale
agents (no heartbeat > `agent_ttl`) expire lazily. `work op=start`
auto-claims its item + targets.

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

## Fold map (previous 11-tool surface → 7 lifecycle tools)

`sym`→`find scope=code` · `map`→`get <dir>` · `impact`→`get depth` ·
`contracts`/`plan_change`→`draft` context pack + `get` · `lint_ears`+
`coverage`→`check` · `link`→`rule applies` · `add_rule`/`rm_rule`→`rule` ·
`reindex`→automatic sync (CLI `spectacle reindex` for debugging).

## Status

Everything above is live except graph-backed parts (`find scope=code`,
`get depth`, `#impact`, anchor spans), which return documented
empty/pending results until the M1 indexer fills the graph.
