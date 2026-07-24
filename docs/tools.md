# MCP tool surface

Sixteen orthogonal tools (eleven lifecycle + three swarm + one codegen + one
fleet). The Go structs in `internal/mcpserver/tools.go` (and, for `decide`,
`commands` and `knowledge`, their sibling files `decide.go`/`commands.go`/
`knowledge.go`) are the normative schema source (SPX-REPO-001 keeps this
file consistent with them). The server-description (MCP `instructions`, sent
in the initialize handshake) teaches the lifecycle loop — see
`internal/mcpserver/server.go`.

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
- **The LLM never writes .spectackle files** — `draft`, `rule`, `move`,
  `compact` and `knowledge op=apply` are the only write paths, and they are
  server-side; `knowledge op=apply` writes through no path of its own — every
  entry it persists goes through `rule`'s own composer (`spec.AddRule`) or
  `decide`'s own ADR persistence (`lifecycle.Draft` + `item.Upsert`).

## Output line grammar

```
n <id> <kind> <file>:<line>[-<endline>] [sig=<sig>] node (endline shown when known and > line)
e <src> <ekind> <dst> [via=<file>:<line>]        edge (call|incl|cgo|asm|launch|use|link)
r <ruleID> <P> <scopeDir> <text>                 rule (P: U|E|S|N|O|C)
r-root <ID> <ID> ...                             root-scoped rules, IDs only (full text via get)
i <id> <kind> <state> <dir> <title>              lifecycle item (state: draft|submitted|approved|active|done|archived|rejected|blocked)
refs <id> <id> ...                               item citations (draft refs=; get renders when non-empty, MCP-012)
s sec:<dir>#<name> <text>                        prose section
j <ref> <summary> :: <snippet>                   journal/history record
a <rule> <node> <file>:<s>-<e> <chash>           anchor
d <cls> <rule> <node> <file>:<s>-<e> [item=<id>] drift (gone|stale)
d healed <rule> <node> <file>:<s>-<e> was=<h> now=<h>  drift, mechanically healed (evolved)
d audit <rule> <node> <file>:<s>-<e> <cls>       drift, never healed (tightened|diverged)
g <kind> <ref> <msg>                             gap (uncovered|orphan)
cf <kind> <key> n=<count>                        knowledge merge conflict (same identity, different substance)
cf> count=<n> <preview> sources=<repo,...>        one conflicting answer inside a cf record
c <dir> <reason> <n>                             compact candidate
sb <msg>                                         stale-binary hint (postCall piggyback, T-0115)
! <code> <sev> <ref> <msg>                       finding (lint E001-E101, LEASE, WT, GATE, LOCK, GRILL, NEEDS)
ag <name> <item|-> <hb-age>s <wt|main>           agent
l <path> <agent> <item|-> <exp>s                 scope lease
h <harness> <marker>                             detected harness (commands op=detect; claude|copilot|codex|kimi)
sw <seq> <agent> <ev> <ref|-> <msg>              swarm event (sibling learning, may prefix ANY result)
wt <item> <state> <root>                         worktree (open|gating|integrating|conflict|replaying)
need <slot> <question>                           missing input (elicitation fallback)
q <ref> <question>                               open question (research #open, grill #questions)
q free <id> <title>                              swarm claimable queue: approved item, no lease collision
q held <id> <agent> <path>                       swarm claimable queue: item blocked by <agent>'s lease on <path>
q more n=<count>                                 swarm claimable queue truncated (cap 20)
b <id> <issue>                                   brief-quality finding (grill #briefs: child task body fails the exhaustiveness heuristic)
nf <id> <id> <id>                                not found — nearest matches
cur <token>                                      more results; pass back as cur
ok [<msg>]                                       success / nothing to report
#impact #contracts #rejections                   context-pack sections (draft)
#impact #contracts #rejections #history #docs #gaps #open  research pack sections
#targets #contracts #briefs #tests #rejections #questions  grill pack sections
#version #items #rules #graph #swarm #drift #health  snapshot sections (state)
```

Item headers may also carry `rounds: n/max` (server-only reopen/gate-fail
counter), `grilled: <YYYY-MM-DD>` (last `grill` stamp), and `needs:
<id>[, <id>…]` (open `research`/`decision` dependencies) — visible via
`get`/`state`/`i` context, not separate grammar lines.

## Tools

### 1. `find` — unified search (code + every lifecycle subcategory)

```json
{"type":"object","required":["q"],"properties":{
  "q":    {"type":"string"},
  "scope":{"enum":["code","rule","spec","proposal","task","bug","research","adr","rejection","history","all"],"default":"all"},
  "k":    {"type":"integer","default":8},
  "focus":{"type":"string","default":""},
  "budget":{"type":"integer","default":2000},
  "cur":  {"type":"string","default":""}}}
```
`code`→graph, everything else→FTS5. **`rejection` and `history` are the
learn-before-planning scopes** — the loop starts here. `focus` (scope=code
only, SPX-GRA-004) re-ranks matches by deterministic personalized PageRank
seeded at that node — "near what I'm working on" beats global degree rank;
empty keeps the global ordering, an unknown focus answers `nf`. Every read
tool takes `budget`+`cur`: results truncate at record boundaries with a
trailing `cur` record, and passing that token back resumes at the next
record — consecutive pages concatenate without overlap or gap
(SPX-ARC-002, SPX-ARC-006).

### 2. `get` — read one thing by ID

```json
{"type":"object","required":["id"],"properties":{
  "id":    {"type":"string","description":"node|rule|item|sec:<dir>#<name>|path"},
  "depth": {"type":"integer","default":0},
  "budget":{"type":"integer","default":2000},
  "cur":   {"type":"string"}}}
```
Dispatch on ID shape: item→header+body (plus a `refs <ids>` line when the
item carries citations — see `draft`); rule→text+rationale+`a` anchors;
node with `depth>0`→cross-language impact radius (`n`/`e`, BFS); dir→scoped
rules+items; file→resolved contracts; unknown→`nf`. Node results end with
the requested node's binding contracts (SPX-SPC-007): applies-bound and
file-cascade rules as `r` records, root-scoped ones collapsed to one
`r-root` ID record; impact neighbors stay bare.

### 3. `draft` — create a lifecycle item (state=draft)

```json
{"type":"object","required":["kind","title"],"properties":{
  "kind":   {"enum":["proposal","task","research","bug","adr"]},
  "title":  {"type":"string"},
  "body":   {"type":"string"},
  "targets":{"type":"array","items":{"type":"string"}},
  "parent": {"type":"string"},
  "refs":   {"type":"array","items":{"type":"string"}},
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

`refs` cites other items — any kind to any kind, no lifecycle meaning of its
own, unlike `parent` (structural ownership) or `needs` (blocked-on, see
`move`). Every ID is validated against the items the server can currently
load (MCP-012); unknown, malformed or self-referencing IDs reject the whole
call and roll it back — the freshly minted item and its create event are
removed, so nothing is persisted — returning `! ARG E - unknown refs:
<ids>`. `get` renders a `refs <ids>` line whenever the item carries any.

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

Two side-effects piggyback on `move`, both server-counted — never LLM
bookkeeping. Reopening a `done` item back to `active`, and a `work
op=submit` gate failure, each increment the item's `rounds` header field
(default max 3, `config.yaml feedback.max_rounds`). At `rounds ==
max_rounds` the server (never the LLM) side-steps the item to **`blocked`**
— a side-state like `rejected`: outside the total order, never visited on
the happy path, absent from `to`'s enum (no tool call can enter or leave it
directly) — and mints an `adr` item (`ADR-xxxx`, options exactly
`rescope`/`reject`/`override-once`) linked via `needs: ADR-xxxx`; `i`/`state`
show it, `next` and fanout skip it structurally. The only exits, applied by
the server from the matching `decide` answer: `rescope` → `draft`
(mandatory rescoping), `reject` → `rejected` (note = the decide rationale),
`override-once` → `active` (counter reset, exactly once — a second
escalation on the same item offers no override). Separately, a forward
`move` on a `proposal` without a `grilled:` header (see `grill`) or with
unresolved `needs:` (see `research`/`decide`) returns a `! GRILL W` /
`! NEEDS W` warning by default (`config.yaml feedback.grill: warn|require`
tightens it to a hard block); `next` and fanout skip items with open
`needs:` structurally regardless of the warning mode.

### 6. `check` — verify (drift, coverage, lint, compact-due)

```json
{"type":"object","properties":{
  "path":  {"type":"string"},
  "fix":   {"type":"boolean","default":false},
  "budget":{"type":"integer","default":1500},
  "cur":   {"type":"string","default":""}}}
```
Emits `!` lint findings, `g` coverage gaps (`g uncovered <dir>` — source
files with zero applicable rules; `g orphan <rule> <node>` — a live rule's
applies target with no anchors.tsv row, MCP-004), `d` drift records (anchor
classification; position-only moves are silently refreshed), `E101`
duplicate item IDs (branch-merge backstop), `c` compact-due signals.
`fix=true` drafts one backprop proposal per drifted rule (gone, tightened,
diverged) and re-stamps anchors. Run until `ok` before `move to=done`.

**Drift classification is direction-aware (T-0086):** each anchor row
carries both a code-span hash and a rule-sentence hash, so `check` reads
two independent axes — did the code change, did the rule sentence change —
instead of one blended "changed" bucket:

| code \ rule | same             | changed                    |
|-------------|------------------|-----------------------------|
| same        | `ok` / `moved`   | `tightened` — audited only  |
| changed     | `evolved` — healed | `diverged` — audited only |

Only **evolved** (code moved, rule sentence identical) is mechanically
healable: the rule still describes the code correctly, only the anchor's
recorded code hash is stale, so `check` re-stamps it unconditionally (no
`fix` needed — this never touches the spec) and emits one
`d healed <rule> <node> <file>:<s>-<e> was=<old 8-hex> now=<new 8-hex>`
record per healed anchor, plus an `evolved`→`healed` journal event
(auditable history of every silent re-stamp). **`tightened` and `diverged`
are never auto-healed** — the rule's sentence itself changed, which means
either the spec author's intent moved or the anchor is stale in a way a
human has to judge; these surface as
`d audit <rule> <node> <file>:<s>-<e> <tightened|diverged>` and, with
`fix=true`, also get a backprop proposal drafted (same as `gone`).

After the per-anchor records, `check` emits exactly one deduped
`r <id> <pattern> <dir> <text>` line per distinct rule that appeared in a
`d healed` or `d audit` record (never repeated even if the rule anchors
several drifted nodes), followed by a trailer
`ok healed=<N> audit=<M>` whenever at least one heal or audit happened.

### 7. `compact` — housekeeping (dry-run by default)

```json
{"type":"object","properties":{
  "path": {"type":"string"},
  "apply":{"type":"boolean","default":false}}}
```
Candidates: done-unarchived items (apply archives them), journal folds over
`journal_max`, and mergeable rule pairs — `c <dir> mergeable <ID1>+<ID2>
j=<score>` for same-file, same-pattern rules with sentence-token Jaccard
≥ 0.6 or identical non-empty applies sets (MCP-005; suggestion only,
`apply=true` never merges rules). Folds drop `create/move/rule/drift`
noise; **`reject`, `archive` and `compact` events are never dropped**.

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
`start`: lease scope, create worktree + branch `spectackle/<item>` under
`.spectackle/wt/`, mirror live spec state in, re-root the session — the `wt`
line names YOUR edit/build root; spectackle paths stay repo-relative.
`submit`: gate (config `verify:` + item `goal:`) → **code-only commit**
(`.spectackle` excluded — SPX-SWM-001) → merge main into the branch →
re-gate → `--ff-only` to main → **semantic replay** of the .spectackle delta
→ teardown. Conflicts come back as `! WT E conflict <files>` — resolve in
the worktree, submit again (resumable). `abort`: teardown, item back to
approved.

### 10. `swarm` — sibling awareness (zero params)

```json
{"type":"object","properties":{}}
```
→ `ag` agents, `l` leases, `wt` open worktrees, a claimable-work queue, `sw`
recent learnings. The queue (T-0121, MCP-013) is one record per candidate —
every `approved` item, plus every `active` item that carries no live lease
of its own (a crashed or abandoned agent's work, otherwise invisible): `q
free <id> <title>` when no held lease collides with the item's scope
(targets, or its context dir if it has none), `q held <id> <agent> <path>`
naming the holder and the colliding path when one does; sorted by ID,
truncated at 20 with a trailing `q more n=<count>` line. `find
scope=rejection` unions live sibling rejections before they ever merge
(SPX-SWM-002).

Two more proactive hints piggyback onto **every** tool result via `postCall`
(not only `swarm`'s own), each prepended at most once per crossing: unseen
`sw` events (realtime piggyback), and — once the running binary is older
than the newest `.go` file under a workspace that is spectackle's own
module checkout — `sb stale — code changed since build; rebuild+restart:
make dev` (T-0115). The hint re-arms only after a rebuild; it never fires
against a workspace serving a third-party module.

### 11. `state` — one read-only structured snapshot

```json
{"type":"object","properties":{
  "path":  {"type":"string","description":"subtree, default all"},
  "budget":{"type":"integer","default":2000},
  "cur":   {"type":"string","default":""}}}
```
The full spec-driven-development picture in one call, strictly read-only —
unlike `check`, it writes nothing (no `drift.Save`, no backprop drafts, no
journal, no anchor re-stamp). Sections, each omitted entirely when it has
nothing to report (SPX-MCP-004 spirit): `#version` (server version, agent
name, active root), `#items` (counts by state + `i` lines, scoped to
`path`), `#rules` (per-context-dir rule counts + a global lint-findings
count), `#graph` (`g.Stats()` node/edge totals), `#swarm` (`ag`/`l`/`wt`
lines), `#drift` (anchor classification summary + bare `d <cls> ...` lines
for evolved/tightened/diverged/gone/stale — `moved` anchors are counted,
never silently re-stamped, and unlike `check` nothing here is ever healed
or audited-with-a-backprop-draft: `state` is read-only, so evolved anchors
just show up as `d evolved ...` instead of the `d healed`/`r`/trailer
records `check` produces), `#health` (compact-due `c` lines + a
coverage-gap count).
Budget-truncated like every other read tool (SPX-ARC-002). Same content is
exposed as the `state` MCP prompt (`internal/mcpserver/prompts.go`) via the
shared `(s *Server) stateText(path string)` builder.

### 12. `research` — condensed problem-space pack (server-aggregated, read-only)

```json
{"type":"object","required":["q"],"properties":{
  "q":      {"type":"string","description":"topic, node ID, or item ID"},
  "targets":{"type":"array","items":{"type":"string"},"description":"optional node IDs/paths to seed impact"},
  "depth":  {"type":"integer","default":2},
  "budget": {"type":"integer","default":2500},
  "cur":    {"type":"string","default":""}}}
```
Stage 1 of research: the server aggregates what it already knows into one
condensed pack of dense records — never file contents — so the
orchestrator's context grows by O(pack), not O(codebase): `#impact` (graph
impact radius, IDs+spans), `#contracts` (binding EARS rules), `#rejections`
(similar past failures), `#history` (journal FTS), `#docs` (prose-section
FTS hits), `#gaps` (unanchored targets, uncovered dirs), `#open` (`q`
records — server-generated open questions, e.g. `q target <id> has no
binding rule`). Empty sections are omitted (SPX-MCP-004 spirit). Strictly
read-only, same "server aggregates, LLM reads IDs" contract as `draft`'s
context pack. Stage 2: when the pack doesn't answer the question (external
knowledge, a measurement nothing in the repo can supply), `draft
kind=research` mints an `R-xxxx` item with an exhaustive brief and
delegates it to a fresh, cheap subagent exactly like any other task — never
ad hoc exploration in the orchestrator's own context. The result is a doc
file plus a condensed summary in the item body; the orchestrator reads only
the summary via `get R-xxxx`. Naming note: `research` is both a tool
(deterministic, stage 1) and an item kind (delegated, stage 2) — same word,
two levels of the same activity, by design.

### 13. `grill` — critique pack before delegation

```json
{"type":"object","required":["id"],"properties":{
  "id":    {"type":"string","description":"proposal or task ID"},
  "budget":{"type":"integer","default":1500},
  "cur":   {"type":"string","default":""}}}
```
Server-computed evidence for the questioning an orchestrator should do
before approving or delegating a plan — the critique itself is LLM
reasoning; `grill` only supplies the material: `#targets` (`nf` for
unknown/unanchored targets), `#contracts` (targets with no binding rule),
`#briefs` (`b` records — child task bodies that fail the exhaustiveness
heuristic: under 300 chars, no file path, no verification command, no
scope sentence), `#tests` (target packages with no `*_test.go`,
SPX-TST-001), `#rejections` (similar past failures), `#questions` (`q`
records — a grill checklist). Writes exactly one journal event `ev=grill`,
which stamps the item header field `grilled: <YYYY-MM-DD>` — the O(1),
compact-fold-surviving evidence a forward `move` checks for (see `move`
above).

### 14. `decide` — native, persistent user decisions

```json
{"type":"object","required":["op"],"properties":{
  "op":      {"enum":["ask","answer","ls"]},
  "id":      {"type":"string","description":"ADR-id (answer) — omit for ask"},
  "question":{"type":"string","description":"ask: the decision to make"},
  "context": {"type":"string","description":"ask: ADR context — the forces and constraints behind this decision"},
  "kind":    {"enum":["radio","confirm","text"],"default":"radio"},
  "options": {"type":"array","items":{"type":"string"},"description":"radio choices, 2-5"},
  "item":    {"type":"string","description":"lifecycle item this decision blocks"},
  "choose":  {"type":"string","description":"answer: option text / yes|no / free text"},
  "consequences":{"type":"string","description":"answer: ADR consequences — trade-offs and follow-on effects of the decision"}}}
```
`ask` tries MCP elicitation (`Session.Elicit`, the same native-UI mechanism
`rule`'s slot forms already use in production — `elicitSlots` in
`tools.go`) — `radio`→enum property (host renders a radio/dropdown),
`confirm`→boolean property (confirm dialog), `text`→string property (free
text). Two outcomes: **the host renders it and the user answers** — the
decision is persisted immediately (`ADR-xxxx` item → `done` with the choice),
returns `ok ADR-x <choice>`. **No elicitation support, declined/cancelled, or
a different harness** — the `ADR-xxxx` item stays open (`state=submitted`),
returns `need decision ADR-x <question> | <options>`; the orchestrator does
**not** block on it, it keeps working other disjoint tasks. `answer`: from
any session, any time, validated against `options` — decisions get
answered from wherever, whenever; the waiting orchestrator sees the answer
on its next `swarm` (sw-piggyback) or `state`/`find` call. `ls`: lists open
`ADR` items. New item kind `adr` (ID letter `ADR`, `find scope=adr`) — architecture decision records are first-class, searchable items. Each ADR captures four structured fields following the classic ADR template: **Context** (forces and constraints behind the decision), **Decision** (the chosen option), **Consequences** (trade-offs and follow-on effects), and **Status** (proposed/accepted/superseded/deprecated) — queryable via `find scope=adr`, drift-anchored like any other record, never loose markdown.
Every decision that actually needs the user goes through `decide` — never
unstructured chat.

### 15. `commands` — generate harness-native slash-command/prompt files

```json
{"type":"object","required":["op"],"properties":{
  "op":      {"enum":["detect","gen"]},
  "harness": {"type":"array","items":{"enum":["claude","copilot","codex","kimi"]},"description":"omit to auto-detect"},
  "commands":{"type":"array","items":{"type":"string"},"description":"gen only: opt-in command names to add on top of the default three (find|get|research|swarm|export|merge) — omit for defaults only"},
  "all":     {"type":"boolean","description":"gen only: generate every command (default three plus all six opt-in exploration commands)"}}}
```
Regenerates harness-native entry points from nine templates
(`internal/mcpserver/templates/commands/*.md.tmpl`, `go:embed` +
`text/template`) instead of hand-maintaining N per-harness copies. `gen`'s
command set defaults to the three load-bearing commands: the two-mode
(`$ARGUMENTS` empty → state snapshot, non-empty → full SDD lifecycle) entry
point (the same content `.claude/commands/spectackle.md`/`spectackle-state.md`
carry today), `state`, and `generate` itself — `generate` has to be in the
default set, otherwise nobody with only the default install can ask for the
rest. `all=true`, or `commands=` naming any of the six opt-in exploration
commands (`find`, `get`, `research`, `swarm`, `export`, `merge` — each
exposing a tool the calling agent already has, so generating them into
every consuming repo's harness directory is opt-in on purpose), adds those
on top of the default three — the requested set **unions** with the
defaults, it never replaces them. `detect`: sniff root markers and emit one
`h <harness> <marker>` line per hit — `.claude/` → claude; `.github/prompts/`
or `.github/copilot-instructions.md` → copilot; `.codex/` → codex; `.kimi/`
→ kimi; `AGENTS.md` → both codex and kimi (they share it); no hits → `nf
harness — pass harness=... or answer the decision`.
`gen`: the harness set resolves **arg > detection > elicitation** — an
explicit `harness=` wins, else `detect`'s hits, else a native checkbox form
(`Session.Elicit`, one boolean per harness — the same mechanism `elicitSlots`
in `tools.go` and `decide op=ask` in `decide.go` use); no elicitation
capability, decline, cancel, or a different harness leaves a free-text
`adr` item open (`need decision ADR-x …`) exactly like `decide op=ask`'s
own no-UI fallback — `commands gen` never blocks or guesses. Per-dialect
output, one file per selected command: **claude** → `.claude/commands/spectackle.md`
(the unnamed entry point) plus `spectackle-<name>.md` for every other
selected command — `spectackle-state.md` and `spectackle-generate.md` by
default, plus `spectackle-find.md`/`-get.md`/`-research.md`/`-swarm.md`/
`-export.md`/`-merge.md` when requested (`description:` frontmatter, as
today). **copilot** → the same set as
`.github/prompts/spectackle[-<name>].prompt.md` (`mode: agent`
frontmatter). **codex**/**kimi** → one managed section in `AGENTS.md`,
delimited by `<!-- spectackle:commands:begin -->`/`<!-- …:end -->`, one `##
spectackle <heading>` subsection per selected command — created if
`AGENTS.md` is missing, otherwise only the managed section is replaced,
never the rest of the file; a subsection an earlier run wrote that this
run's command set doesn't ask for again is carried forward unchanged rather
than dropped, so a default-only run after an `all=true` run leaves the
opt-in subsections intact. Every generated artifact carries a `<!--
generated by spectackle \`commands\` — edit internal/mcpserver/templates,
not this file -->` header. Re-running `gen` with the same harness+command
set is idempotent (byte-identical output). Writes go straight to disk
(`os.WriteFile`) — these are generated repo files, not `.spectackle/`
lifecycle state: no journal event, just one coord `commands` emit so
siblings see it happened in realtime.

### 16. `knowledge` — fleet-portable knowledge (export, merge, apply)

```json
{"type":"object","required":["op"],"properties":{
  "op":       {"enum":["export","merge","apply"]},
  "source":   {"type":"string","description":"export: repository label; default this binary's own module path"},
  "entries":  {"type":"array","items":{"type":"object"},"description":"export brownfield path: caller-authored entries — kind, payload fields, asserted_by/derived_from source labels"},
  "out":      {"type":"string","description":"export: also write the marshaled artifact to this path"},
  "paths":    {"type":"array","items":{"type":"string"},"description":"merge|apply: artifact file paths to read"},
  "artifacts":{"type":"array","items":{"type":"string"},"description":"merge|apply: inline marshaled artifacts"},
  "dir":      {"type":"string","description":"apply: context dir every added entry lands in, default root"},
  "stem":     {"type":"string","description":"apply: rule ID stem for added rules; default derived from the artifact's first source"}}}
```

One noun, three ops on `internal/knowledge`'s artifact format (rules, ADRs,
whitelisted prose sections — see that package's doc comment), matching how
`decide`, `rule`, `lease` and `work` already put multiple operations behind
one tool name instead of growing the tool count.

**`export`** produces this workspace's artifact. Two modes: with no
`entries`, walks the cascade and items via `knowledge.Extract` — `source`
defaults to this binary's own module path, derived the same way
`moduleRepoURL` derives its `https://` URL (`debug.ReadBuildInfo`, never
hardcoded). With `entries`, the **brownfield** path for a repository with no
`.spectackle` bundle at all: an LLM that surveyed code/tests/docs authors
entries directly, and every one is routed through `knowledge.NewEntry` —
validated and content-keyed exactly like an Extracted entry; there is no
key/id field on an entry, a caller cannot supply one even by mistake. The
marshaled artifact is always returned inline, and `out=` additionally writes
it to a path (a fleet workflow needs a file to move between repositories) —
refused if that path falls inside `.spectackle` (server territory,
SPX-ARC-005).

**`merge`** parses N artifacts (`paths=` file paths and/or `artifacts=`
inline text, either or both), folds them with `knowledge.Merge`, and returns
the condensate plus every conflict as dense `cf` records — conflicts are
reported, never auto-resolved:

```
cf <kind> <key> n=<count>
cf> count=<n> decision="..."|text="..."|prose="..." sources=<repo,repo,...>
```

one `cf>` line per distinct answer inside the conflict.

**`apply`** folds exactly one artifact (`paths`/`artifacts` — cardinality
other than 1 is a caller error) into this workspace. **Additive only**: adds
what the workspace lacks, never deletes, never overwrites a local
specialization — an identity (`Kind`+content key) the workspace already
carries is skipped wholesale, its substance never compared, so a
same-question-different-answer ADR already resolved locally stays exactly as
this repo decided it. **Idempotent**: applying the same artifact twice adds
nothing the second time. Dedup is on the **content key**, never on rule ID —
the receiving repo mints its own IDs (`spec.AddRule`'s usual minting, `dir=`
+ `stem=`, default stem derived from the artifact's first source label), so
the same sentence arriving twice has to be recognized by what it says.
`internal/knowledge/apply.go`'s pure diff function is named **`FoldInto`**,
not `Apply` — `knowledge.Apply` already means "fold a conflict Resolution
into an artifact" (a different operation on a different pair of types);
`FoldInto` folds an Artifact into a workspace's own knowledge instead.
**No new write path**: a rule entry goes through the exact composer `rule
op=add` uses (`spec.AddRule` — lints, auto-IDs, appends to the scoped
spec.md); an ADR entry persists exactly like `decide`'s own resolved-decision
outcome (`lifecycle.Draft` + `item.Upsert`, state set straight to `done`
since the decision already happened in the artifact's origin repo — there is
no question left to elicit). An applied rule carries **no `applies`
binding** on purpose, so it anchors nothing yet — `check`'s coverage-gap
pass (`g uncovered`, untouched by this tool) is exactly the adoption
worklist that leaves open. `apply`'s own trailer echoes that same count in
the same call, so the caller does not need a separate `check` round trip to
see it:

```
ok added=<N> skipped=<S> gaps=<G> [unsupported=<U>]
```

`gaps` is `check`'s own whole-workspace coverage-gap count (`g uncovered`),
recomputed against the freshly written cascade — not a delta this call
caused (an applied rule, once it lands under `dir=`, can only ever close a
coverage gap there, never open one; the number is "how much of the adoption
worklist is still open", not "how many gaps this call opened"). Contract:
MCP-009. `unsupported` (only shown when nonzero) counts `intent` entries in
the incoming artifact: `Extract` keys a whole prose section as one entry,
but the only existing write path for `## intent` (`spec.AppendIntent`) is
line-additive, so folding a whole-section blob through it would break
`FoldInto`'s own idempotence contract on a second apply (the re-extracted,
now-combined section hashes to a different key than the standalone entry
that went in) — deliberately left unfolded rather than silently wrong.

### 17. Prompts — slash-command entry points

Three MCP prompts (`prompts/get`, no arguments unless noted) in
`internal/mcpserver/prompts.go`, registered by `(s *Server) registerPrompts()`
(not yet wired into `New` — the orchestrator calls it). They bypass `gate()`
(prompts/get is not a tool call): each handler locks `s.mu` and calls
`s.scan.Refresh()` itself so the snapshot below is current.

**`workflow`** (optional `task` string arg) — a standing situational-awareness
dump: line 1 `spectackle workflow - state below is live`, then
`AGENTS/LEASES` (`ag`/`l` lines from `s.cd.Agents()`/`s.cd.Leases()`),
`ACTIVE ITEMS` (`i` lines from `item.LoadAll`, non-draft items surfaced
first), and `LOOP` — the six-step lifecycle checklist condensed from the
server's `instructions` manifest. With `task` given, the response instead
carries the same two-mode lifecycle instruction the `/spectackle <task>`
repo command drives (research → draft → grill → decide-if-uncertain →
approve → fan out → check → archive) with the task text embedded — this is
the MCP-native form of the two-mode entry point, so it works identically in
any MCP harness, not only Claude Code's repo commands.

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
(`/mcp__spectackle__state`) instead of a tool call.

## Fold map (previous 11-tool surface → 7 lifecycle tools)

`sym`→`find scope=code` · `map`→`get <dir>` · `impact`→`get depth` ·
`contracts`/`plan_change`→`draft` context pack + `get` · `lint_ears`+
`coverage`→`check` · `link`→`rule applies` · `add_rule`/`rm_rule`→`rule` ·
`reindex`→automatic sync (CLI `spectackle reindex` for debugging).

## Status

Everything above is live except graph-backed parts (`find scope=code`,
`get depth`, `#impact`, anchor spans), which return documented
empty/pending results until the M1 indexer fills the graph.
