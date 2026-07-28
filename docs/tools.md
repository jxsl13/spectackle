# MCP tool surface

Eighteen orthogonal tools (twelve lifecycle + three swarm + one codegen,
one portability, one benchmark). The Go structs in `internal/mcpserver/tools.go` are
the normative schema source (SPX-REPO-001 keeps this file consistent with
them). The server-description (MCP `instructions`, sent in the initialize
handshake) teaches the lifecycle loop — see `internal/mcpserver/server.go`.

## Design principles

- **Stable short IDs are the currency**: nodes `go:saxpy.Saxpy`, rules
  `CUDA-KRN-001`, items `P-01KYD3ZQ8MF8`, sections `sec:gpu#intent`. The LLM
  names concepts, never file paths or contents.
- **Item IDs are prefixes, everywhere an item ID is taken** (ADR-0013). A
  record ID is stored in full — the kind letter plus a 26-character
  UUIDv7 in Crockford base32, which makes it globally unique without
  coordination and sortable by mint time. Every tool argument that takes an
  item ID (`get`, `move`, `grill`, `draft`'s `parent`/`refs`, `decide`'s
  `item`/`id`, `work`, `lease`) accepts **either the full ID or any
  unambiguous leading piece of it**, and every result **emits** the shortest
  currently-unambiguous prefix, so an ID copied out of one result is accepted
  back verbatim by the next call. Three outcomes, never a guess:
  a prefix matching one record resolves; one matching several refuses with
  `! ARG E <prefix> ambiguous prefix — N records: …`, naming every candidate
  so you can disambiguate in one more call; one matching nothing keeps the
  ordinary `nf` behavior with nearest matches. Two consequences worth
  knowing: the emitted length is computed per call and **grows** as records
  accumulate, so a prefix captured early can turn ambiguous later — re-read
  it from a fresh result, or keep the full ID; and legacy `P-0007`-style
  sequential IDs stay valid forever, because archived records live on only
  as journal tombstones addressed by ID.
- **Few tools, flat params, enums + defaults** (SPX-ARC-004): the common call
  is one or two fields; no nesting, no option floods.
- **Dense line records, not JSON, in results** (SPX-MCP-002).
- **Budgets and cursors** on read tools (SPX-ARC-002).
- **Corrections instead of errors** (SPX-ARC-003): unknown ID → `nf` with
  the nearest matches.
- **The LLM never writes .spectackle files** — `draft`, `rule`, `move`,
  `compact` are the only write paths, and they are server-side.

## Output line grammar

```
n <id> <kind> <file>:<line>[-<endline>] [sig=<sig>] node (endline shown when known and > line)
e <src> <ekind> <dst> [via=<file>:<line>]        edge (call|incl|cgo|asm|launch|use|link)
r <ruleID> <P> <scopeDir> <text>                 rule (P: U|E|S|N|O|C)
r-root <ID> <ID> ...                             root-scoped rules, IDs only (full text via get)
i <id> <kind> <state> <dir> <title>              lifecycle item (state: draft|submitted|approved|active|done|archived|rejected|blocked)
s sec:<dir>#<name> <text>                        prose section
j <ref> <summary> :: <snippet>                   journal/history record
a <rule> <node> <file>:<s>-<e> <chash>           anchor
d <cls> <rule> <node> <file>:<s>-<e> [item=<id>] drift (gone|stale)
d healed <rule> <node> <file>:<s>-<e> was=<h> now=<h>  drift, mechanically healed (evolved)
d audit <rule> <node> <file>:<s>-<e> <cls>       drift, never healed (tightened|diverged)
g <kind> <ref> <msg>                             gap (uncovered|orphan)
m <id> v<n> <name> ...                           benchmark record (bench; f/u/d sublines — see tool 17)
x <kind> <key> src=<repo,repo> <summary>         merge conflict (knowledge op=merge, one line per competing entry, NEVER auto-resolved)
c <dir> <reason> <n>                             compact candidate
! <code> <sev> <ref> <msg>                       finding (lint E001-E101, LEASE, WT, GATE, LOCK, GRILL, NEEDS, TYPED, VAC)
ag <name> <item|-> <hb-age>m <wt|main>           agent (heartbeat age, floored to minutes)
l <path> <agent> <item|-> <exp>m                 scope lease (time left, floored to minutes)
h <harness> <marker>                             detected harness (commands op=detect; claude|copilot|codex|kimi)
sw <seq> <agent> <ev> <ref|-> <msg>              swarm event (sibling learning, may prefix ANY result)
wt <item> <state> <root>                         worktree (open|gating|integrating|conflict|replaying)
need <slot> <question>                           missing input for the calling agent to supply
q <ref> <question>                               open question (research #open, grill #questions)
b <id> <issue>                                   brief-quality finding (grill #briefs: child task body fails the exhaustiveness heuristic)
nf <id> <id> <id>                                not found — nearest matches
cur <token>                                      more results; pass back as cur
ok [<msg>]                                       success / nothing to report
#impact #contracts #rejections                   context-pack sections (draft)
#impact #contracts #rejections #history #docs #gaps #open  research pack sections
#targets #contracts #tests #rejections #questions #computed #evidence  grill pack sections
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
Dispatch on ID shape: item→header+body; rule→text+rationale+`a` anchors;
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
  "dir":    {"type":"string"},
  "refs":   {"type":"array","items":{"type":"string"}}}}
```
Server assigns ID (`P-0001`…) and context dir (targets→deepest common
context, else root). With `targets` the response is the **context pack**:
`#impact` (radius), `#contracts` (binding EARS rules), `#rejections`
(similar past failures) — the synergy moment, one round trip. Root-scoped
rules collapse into a single `r-root` ID-only line instead of repeating
their full text every draft, and any of the three sections with nothing to
report is omitted outright rather than filled with an `ok` placeholder
(SPX-MCP-004).

`refs` cites other item IDs this item draws on — research, an ADR, a prior
proposal, any kind — and is validated before the item is persisted: an ID
that resolves to neither a live item nor a journal-tombstoned (archived)
one refuses with `! ARG E - unknown refs: ...` and writes nothing. A
citation to an archived item is legitimate (its outcome lives in the
journal, per `find scope=history`) and passes. `get` on an item with refs
renders a `refs <id> <id> ...` line after `rules`; `grill` on a `proposal`
with no `ADR-`/`R-` ref and no rejected-alternative prose in its body asks
`q no deliberation recorded: no ADR/research ref and no rejected
alternative`.

`draft id=<item>` revises a DRAFT-state item in place — body, title,
targets, refs replace when given. Grill stamps and review verdicts bind the
substance hash and expire with the revision; the review loop's feedback can
finally amend the record it critiques (B-01KYER). From `submitted` on the
body is the frozen review subject and revision is refused.

`#evidence` (task/bug targets): the unconsumed sweep (exported symbols
nothing outside their file calls — B-0009's class) and caller-divergence
(minority argument shapes among 5+ sites — B-0003's class). Suppress a
known-false unconsumed finding per symbol with an `unconsumed-ok: <symbol>
<reason>` body line — visible in the pack, flagged stale when obsolete.

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
spec.md, journals, anchors `applies` for drift. Missing slots are returned
as `need` records **to the calling agent** — the rule's author — never as a
user form (ELICIT-001).
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
In online git mode the pull request stays DRAFT through every edge and
reopen cycle (`g pr N stays draft until archive`, PR-DRAFT-001): the single
draft→ready flip happens at the archive edge immediately before merge, so
review cycles never churn PR state. Offline (the default,
GIT-DEFAULT-001) there is no PR: every edge is a commit on the current
branch (`g offline commit <short-sha> <subject>`).
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
directly) — and mints a `decision` item (`D-xxxx`, options exactly
`rescope`/`reject`/`override-once`) linked via `needs: D-xxxx`; `i`/`state`
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
Emits `!` lint findings, `g` coverage gaps (`g orphan <rule> <node>` — a
live rule's applies target with no anchors.tsv row, MCP-004; and, ONLY
under `coverage_gate: package` in config.yaml, `g nocontract <dir>` for
each `internal/`/`cmd/` package failing COVERED — no bundle at the dir or
an ancestor below root, and no root rule whose non-empty `applies` binds an
anchored node in its subtree; sorted, capped at 20 plus a `+<n> more` tail),
`d` drift records (anchor classification; position-only moves are silently
refreshed), `E101` duplicate item IDs (branch-merge backstop), `c`
compact-due signals. Without the gate key, check emits nothing for package
coverage — its single `ok` path is full-string-compared by CI self-hosting
gates, so visibility lives in `state` (`ok dir <d> rules=0 uncovered` per
uncovered package), and turning coverage into CI-red findings is an
explicit opt-in.
`fix=true` drafts one backprop proposal per drifted rule (gone, tightened,
diverged) and re-stamps anchors. Run until `ok` before `move to=done`.

Dirty `_test.go` files are scanned by the validate pack's AST vacuous-test
detector in-loop: an assertion-free subtest or an unguarded all-assertions
range renders `! VAC W <file>:<line> <reason>` (capped at 10, `+n more`
tail) while the file is still uncommitted — committed legacy tests stay
quiet, so a clean tree keeps the bare `ok`.

A `! TYPED W - typed-call pass disabled packages=<n>: <cause> ...` finding
appears exactly when the last reindex's go/types call-edge upgrade pass did
not complete (a Go-toolchain mismatch or a broken module) — never on a
healthy pass, so this never adds a line to a clean run. Without it, the
graph silently keeps only the syntactic call edges: cross-package
`get depth`/impact-radius answers under-report and nothing else says so.

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

Anchors re-resolve by CONTENT HASH when their node ID vanished or crossed
files (B-01KYJB3SGK): tilde numerals (`go:main.main~2`) are walk-order
fragile and never trusted — the single candidate under the ID stem whose
span hash matches the stored one wins
(`d rebound <rule> <old> -> <new> <file>:<s>-<e> (hash match)`),
two matches refuse, and an ID that re-binds to an unrelated file with no
hash match anywhere audits as
`d audit <rule> <node> <file>:<s>-<e> crossfile now=<otherfile>` — never
healed, never rebound.

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

**Worktree contention outlives leases** (ADR-01KYKTGGPREG2): a one-shot CLI
deregisters its leases when the process exits, but its worktree — and the
contention it represents — stays open. So `work op=start` additionally
refuses when a live sibling WORKTREE's item declares an overlapping target:
`! LEASE E <path> held=<agent> item=<id> (open worktree)`. The refusal
names its own recoveries — wait for that item's submit/abort, or, when the
holder is gone, `work op=abort item=<id>` releases it (`force=true` also
discards its uncommitted work) — because worktree rows have no TTL: only
submit/abort/adopt clears one. Same-identity siblings are checked too (a
second one-shot process reuses the name with no in-process memory of the
first). The blocked agent never pays the implement-then-conflict-resolve
round the merge layer would otherwise bill it (measured: ~20 wasted calls
vs 1 refused call, M-01KYKSKKPDFNT). Two starts racing inside the
check-then-act window both publish their ledger row, then re-read: any
racer that sees the other YIELDS — rolling its seconds-old, work-free
worktree back and refusing. Yield-always rather than a tiebreak, because
the racer whose re-read lands first sees nothing and proceeds without
comparing; only a rule needing no agreement from the other side is safe.
The symmetric window costs two refusals and a retry, never two worktrees
on one target.

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
→ `ag` agents, `l` leases, `wt` open worktrees, `sw` recent learnings.
Unseen `sw` events are additionally prepended to every tool result
(realtime piggyback); `find scope=rejection` unions live sibling rejections
before they ever merge (SPX-SWM-002).

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
count), `#graph` (`g.Stats()` node/edge totals, plus the `! TYPED` typed-
call-pass finding described under `check` above when that pass is
degraded — omitted on a healthy pass, same gate on both tools), `#swarm`
(`ag`/`l`/`wt` lines), `#drift` (anchor classification summary + bare `d
<cls> ...` lines
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
  "id":      {"type":"string","description":"proposal or task ID"},
  "op":      {"enum":["pack","verdict"],"default":"pack"},
  "pass":    {"type":"boolean","description":"verdict: true = approved by the reviewer"},
  "findings":{"type":"string","description":"verdict: required on pass=false — they become the author's next brief"},
  "waivers": {"type":"object","description":"verdict: per-finding key→reason; every open finding is fixed or waived"},
  "lenses":  {"type":"string","description":"verdict: comma-separated lenses walked sequentially by ONE reviewer"},
  "panel":   {"type":"integer","description":"verdict: n-agent panel, legal only on a live risk signal, capped by swarm.panel_max"},
  "agent":   {"type":"string","description":"verdict: reviewer identity when the session cannot carry SPECTACKLE_AGENT"},
  "budget":  {"type":"integer","default":1500},
  "cur":     {"type":"string","default":""}}}
```
The default `op=pack` renders the evidence below and stamps
`grilled: <date> open=<n>`. **`op=verdict` records the INDEPENDENT
review** — a second, deliberately named `SPECTACKLE_AGENT`, never the
shared resident identity — and `move to=approved` gates on a passing
verdict bound to the current body hash. The reviewer judges ON the
evidence: every open finding is addressed (fixed, or waived per key with
a recorded reason), then a fresh identity's verdict opens the gate; the
pack never passes or fails anything itself. Verdict events survive
journal compaction (they are the evidence the gates rest on).

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

### 14. `validate` — post-implementation judge (gates archive)

```json
{"type":"object","required":["id"],"properties":{
  "id":      {"type":"string","description":"task or bug ID"},
  "op":      {"enum":["pack","verdict"],"default":"pack"},
  "pass":    {"type":"boolean"},
  "findings":{"type":"string","description":"verdict: required on pass=false — they become the implementer's next brief"},
  "waivers": {"type":"object","description":"verdict: per-finding key→reason"},
  "agent":   {"type":"string","description":"verdict: validator identity when the session cannot carry SPECTACKLE_AGENT"},
  "budget":  {"type":"integer","default":1500},
  "cur":     {"type":"string","default":""}}}
```
`grill` reviews the PLAN; `validate` judges the IMPLEMENTATION. The default
`op=pack` renders the computed pack over the item's REAL diff (commits
citing the item): `#diff` declared-vs-landed, `#computed` `v` findings
(untouched targets, off-scope files, untested symbols, vacuous tests, fake
benchmarks, missing docs), `#verify`. `op=verdict pass=<bool>` records the
INDEPENDENT validation — a second deliberate identity, never the
implementer. **`move to=archived` gates on it**: with `feedback.validate`
at its default the gap renders as an advisory `! VALIDATE W` and the
archive proceeds; under `feedback.validate: require`, or when the risk
gate trips (`feedback.risk_files`, `dangerous_paths`), it becomes a
refusing `! VALIDATE E`. A failing verdict REOPENS `done → active` with
the findings as the next brief, and counts a round.

### 15. `decide` — native, persistent user decisions

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
`ask` tries MCP elicitation (`Session.Elicit` — the ONLY tool that may:
elicitation forms land on the human, and `decide op=ask` is the one call
where the human is the addressee, ELICIT-001) — `radio`→enum property
(host renders a radio/dropdown),
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

### 16. `commands` — generate harness-native slash-command/prompt files

```json
{"type":"object","required":["op"],"properties":{
  "op":     {"enum":["detect","gen"]},
  "harness":{"type":"array","items":{"enum":["claude","copilot","codex","kimi"]},"description":"omit to auto-detect"}}}
```
Regenerates the two-mode (`$ARGUMENTS` empty → state snapshot, non-empty →
full SDD lifecycle) entry point — the same content
`.claude/commands/spectackle.md`/`spectackle-state.md` carry — for every
supported coding-agent harness, from the two templates
(`internal/mcpserver/templates/commands/{workflow,state}.md.tmpl`, `go:embed` +
`text/template`) instead of hand-maintaining N per-harness copies. `detect`:
sniff root markers and emit one `h <harness> <marker>` line per hit — `.claude/`
→ claude; `.github/prompts/` or `.github/copilot-instructions.md` → copilot;
`.codex/` → codex; `.kimi/` → kimi; `AGENTS.md` → both codex and kimi (they
share it); no hits → `nf harness — pass harness=... or answer the decision`.
`gen`: the harness set resolves **arg > detection > open decision** — an
explicit `harness=` wins, else `detect`'s hits, else a free-text `decision`
item is left open (`need decision ADR-x …`) exactly like `decide op=ask`'s
no-UI fallback, answered later from any session — `commands gen` never
blocks, guesses, or pops a form (ELICIT-001). Per-dialect
output: **claude** → `.claude/commands/spectackle.md` +
`spectackle-state.md` (`description:` frontmatter, as today). **copilot** →
`.github/prompts/spectackle.prompt.md` + `spectackle-state.prompt.md`
(`mode: agent` frontmatter). **codex**/**kimi** → one managed section in
`AGENTS.md`, delimited by `<!-- spectackle:commands:begin -->`/`<!-- …:end
-->`, containing both command descriptions — created if `AGENTS.md` is
missing, otherwise only that section is replaced, never the rest of the
file. Every generated artifact carries a `<!-- generated by spectackle
\`commands\` — edit internal/mcpserver/templates, not this file -->` header.
Re-running `gen` with the same harness set is idempotent (byte-identical
output). Writes go straight to disk (`os.WriteFile`) — these are generated
repo files, not `.spectackle/` lifecycle state: no journal event, just one
coord `commands` emit so siblings see it happened in realtime.

### 17. `knowledge` — portable knowledge (export/merge/apply)

```json
{"type":"object","required":["op"],"properties":{
  "op":     {"enum":["export","merge","apply"]},
  "path":   {"type":"string","description":"export: also write the artifact here; apply: read the artifact from this path. Relative = under the workspace root; absolute taken verbatim"},
  "paths":  {"type":"array","items":{"type":"string"},"description":"the artifacts to parse — merge: condense them; apply: fold SEVERAL at once, and their conflicts open decisions instead of vanishing"},
  "body":   {"type":"string","description":"inline artifact text — apply: the artifact to fold in; merge: one more artifact, alongside paths"},
  "entries":{"type":"array","items":{"type":"object","required":["kind"],"properties":{
    "kind":"rule|adr|intent","dir":"string",
    "text":"string","rationale":"string",
    "question":"string","context":"string","decision":"string","consequences":"string",
    "status":"proposed|accepted|superseded|deprecated","options":["string"],
    "prose":"string"}},"description":"export brownfield mode: caller-authored entries for a repo with no .spectackle bundle"}}}
```
One tool, three ops on one noun — internal/knowledge lifts the reusable part
of this repository's spec/item corpus (EARS rules, ADRs, whitelisted prose
sections) into a portable artifact and condenses several such artifacts
into one; this tool is the only wiring between that package and the wire.

`export`: two input modes. **(a)** no `entries` — walks this workspace's
live cascade + items (`knowledge.Extract`); `source` is this module's path,
derived the same way the server's own defect-report URL is (`moduleRepoURL`,
via `debug.ReadBuildInfo`), never hardcoded. **(b)** `entries` supplied —
the **brownfield** path, for a repository with no `.spectackle` bundle at
all, where an LLM surveyed code/tests/docs and authored entries directly:
every entry is routed through `knowledge.NewEntry`, which computes the
content key itself (`drift.NormHash` of the entry's text/question/prose) —
there is no key field on an entry input, so a caller-supplied key is
impossible by construction, and a malformed entry (missing required fields)
rejects with `! ARG E` rather than being coerced. Either mode: the marshaled
artifact is the result body (front-matter-fenced markdown, per
`internal/knowledge`'s own format — never JSON, SPX-MCP-002), followed by an
`ok export entries=<n> rule=<n> adr=<n> intent=<n> [written=<path>]`
trailer; `path` additionally writes the same bytes to disk — a fleet
workflow needs a file to move between repositories.

`merge`: parses N artifacts (`paths`, plus one more inline via `body`),
`knowledge.Merge`s them, and returns the condensate followed by every
conflict as dense `x <kind> <key> src=<repo,repo> <summary>` records — one
line per competing entry sharing an identity (same kind, same content key)
but disagreeing in substance (in practice, always an ADR: two repositories
answering the same question differently). Conflicts are **reported, never
auto-resolved** — curation is a human's call, not this tool's. Trailer:
`ok merge sources=<n> entries=<n> conflicts=<n>`.

`apply`: the only writing operation — folds artifacts (`path`, `paths`
and/or `body`, the same inputs `merge` takes) into this workspace.
**Additive only**: `internal/knowledge`'s `FoldInto`
(named to avoid colliding with `knowledge.Apply`, the unrelated
conflict-resolution fold in `merge.go`) diffs the incoming artifact against
this workspace's own current one (freshly `Extract`ed) and returns only the
entries the workspace lacks, deduped **on content key, not rule ID** — the
receiving repo mints its own IDs, so the same sentence arriving twice is
still recognized. **Idempotent**: applying the same artifact twice adds
nothing the second time — the first call's writes are what the second
call's `Extract` sees as already-current. **No new write path**: a rule
entry goes through `spec.AddRule`, the exact composer `rule op=add` itself
calls, at root scope with no `applies` binding (a portable entry carries
neither a context dir nor node anchors — both are repository-local,
stripped by `Extract`) under a fixed `KB` stem (so imported rules are
recognizable and AddRule always has a stem to mint from, even into an empty
`spec.md`); an ADR entry lands through the same
`lifecycle.Draft`/`item.Upsert`/`journal.Append(EvDecide)` primitives
`decide.go`'s own `resolveDecision` uses (not the `decide op=ask` RPC
itself, which would trigger a live elicitation prompt for a decision that
is already known — wrong for a bulk, already-answered import), landing
directly at `state=done`; an intent/prose entry goes through
`spec.AppendIntent`, the same append-only `## intent` writer
`lifecycle`'s archive step already uses. An applied rule arrives with no
`applies` binding, so `check` has nothing anchored to audit for it yet —
that is intended, not a bug: the anchoring is exactly the adoption work
`check`'s coverage-gap list exists to worklist. Trailer:
`ok applied added=<n> gaps=<n>` — `gaps` is recomputed from the same two
computations `check` itself runs (`g uncovered` + `g orphan`, without
`check`'s side effects), so it is provably the same number a standalone
`check` call reports afterward, never a guess.

**Conflicts become decisions, not casualties** (ADR-01KYMKEG7YE2P).
`merge` reports every conflict as an `x` line and leaves it OUT of the
condensate, so applying that condensate used to land NEITHER side. `apply`
therefore merges its inputs itself, **always** — the non-conflicting union
folds in exactly as a single artifact would, and each conflict opens one
ADR in this workspace through the same path `decide op=ask` uses, rendered
as `need decision <ADR-id> <question>` and counted in the trailer as
`conflicts=<n>`. Its options are the competing decisions labeled by source,
and its body keeps every side, so answering it with `decide op=answer`
lands the winner as an accepted ADR while the losing side stays readable in
the record and — because an `adr` tombstone retains its body and decision
the way a `research` one retains its finding — in the journal after the ADR
is archived. No side is ever adopted automatically.

Merging is unconditional, not gated on how many artifacts arrived: `Merge`
buckets entries across AND within artifacts, and `export` of a workspace
that answered one question twice emits a single artifact carrying both, so
an artifact count is not a conflict count. A conflict-free artifact merges
to itself, which is why the one-artifact render is unchanged.

Minting is idempotent under the same rule the rest of `apply` follows: a
conflict whose `(adr, key)` identity this workspace already holds is
**settled**, not re-asked — whether the decision is one an earlier `apply`
opened, one already answered, one this repository reached on its own, or
one already **archived**. That last case needs a journal pass, because
`Extract` reads `work.md` and an answered ADR's normal end is to leave it:
without it, the one workspace that would be asked its curated questions
again is the one that ran the lifecycle all the way through. Settled
conflicts are counted separately in the trailer as `settled=<n>`, so a
re-apply is silent about work already done without being silent about the
disagreement still in the sources.

### 18. `bench` — benchmark records (implementations on a frame)

```json
{"type":"object","required":["op"],"properties":{
  "op":     {"enum":["put","get","ls","rm","cmp"]},
  "name":   {"type":"string","description":"benchmark name (put: required; ls: substring filter)"},
  "frame":  {"type":"string","description":"k=v dims, space/comma-separated; put requires os arch cpu ram gpu"},
  "results":{"type":"string","description":"put: impl[@src]: metric=value ...; entries ;-separated"},
  "metrics":{"type":"string","description":"name:unit:dir[:noise] declarations; omit to inherit the prior version"},
  "id":     {"type":"string"}, "id2": {"type":"string"},
  "tool":   {"type":"string"}, "note": {"type":"string"}, "dir": {"type":"string"}}}
```

A benchmark entry compares implementations (new/old, Go vs. Python, cuda
vs. cpu-native) on a **frame**: the minimum machine description `os arch
cpu ram gpu` plus any free dims (`impl=vulkan`, `threads=8`). Sentinel
values: `none` = hardware genuinely absent (no GPU), `any` = dimension
irrelevant (machine-independent measurements — token counts, bytes —
collapse into ONE key across hosts). Identity is the canonical key —
folded name + sorted folded dims — so the same name+frame is the same
entry: a `put` re-measures it as a new version. History keeps 1 version
per key by default (`benchmarks.history` in config.yaml raises it); the
superseded head's raw values ride the journal `bench` event, so
regressions stay diagnosable after the trim. Storage is
`.spectackle/bench.ndjson` per context, union-merged like the journal;
unparseable or key-forged lines quarantine (reported by `ls`, preserved
verbatim on save, never dropped).

Line records (`d` here is a bench delta — shape disambiguates from drift):

```
m <id> v<n> <name> ...                           benchmark head/summary
f <k>=<v> <k>=<v> ...                            frame (get)
u <impl> <metric> <value> <unit> [*]             measured value (* = winner under the metric's dir)
d <impl> <metric> <value> <unit> Δ<delta> [better|worse|~]            delta vs. superseded head (put)
d <impl> <metric> <old> -> <new> <unit> Δ<delta> [better|worse|~]     delta between two entries (cmp)
```

- **put** — parses the three grammars, derives the key, assigns the next
  version. Identical content is an idempotent replay (`unchanged`, nothing
  written). A changed head renders one `d` line per (impl, metric) pair
  shared with the outgoing head, then
  `ok m <id> v<n> <name> impls=<n> metrics=<n> better=<n> worse=<n> tie=<n> trimmed=<ver>`.
  Metric directions: `+` higher wins, `-` lower wins, `~` diagnostic
  (never judged); deltas within ±noise render `~` (tie). Omitting
  `metrics` inherits the prior version's declarations; a first `put`
  without them refuses.
- **get** — `m` header, `f` frame, `u` values with the per-metric winner
  starred (only when >1 impl and the metric has a direction).
- **ls** — heads only, filtered by `name` substring and/or `frame`
  subset.
- **cmp** — two entries by ID: `d` lines per shared (impl, metric) pair.
  Units are byte-compared and **never converted** — a unit mismatch on
  the same metric name refuses.
- **rm** — drops every retained version of the record's key (journaled);
  a later `put` of the same name+frame restarts at v1.

### 19. Prompts — slash-command entry points

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
