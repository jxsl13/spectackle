# Agent workflow — orchestrator + fresh implementers

This document specifies the swarm shape spectackle is built for, and the one
this repo itself is developed with (self-hosting, see
[docs/roadmap.md](docs/roadmap.md) milestone 5). It is a product concept,
not just a dev note: the server's tool grammar, lease system, and
instructions text (`internal/mcpserver`) exist to make this division of
labor cheap and safe.

The core bet: **exploration is the most expensive part of agentic coding.**
A model that has to grep the tree, read five files to find the right API,
and reconstruct constraints from scratch burns far more tokens than the
edit itself costs. spectackle attacks this from both ends — `find`/`get`/
context packs replace exploration for the agent that plans, and an
exhaustive task body replaces it for the agent that implements. Because the
brief is written by the complex model, the simple model never has to
explore; that keeps the complex model's own context free of implementation
noise, and it makes token cost scale with the number of tasks rather than
the size of the codebase.

## Roles

The two roles are defined by capability tier, not by vendor or specific
model — an orchestrator is whatever model is complex/strong enough to plan
and brief exhaustively (e.g. a frontier model orchestrating small, fast
models); an implementer is whatever model is cheap enough to run one
disjoint task at a time in parallel.

| | Orchestrator | Implementer |
|---|---|---|
| Model class | complex/strong | simpler/cheaper |
| Context | persistent across the whole item, full repo familiarity | fresh per task — nothing but the task brief + driver recipe |
| Responsibility | draft proposals, write exhaustive task bodies, review implementer output, run gate/verify, merge, archive | pull one approved task, implement it, test it, hand it back |
| git rights | commits, opens PRs, merges | none — never commits, never pushes |
| Tool calls it owns | `draft`, `rule`, `check`, `work op=submit`/`abort`, `move to=approved\|rejected\|archived` | `lease`, `move active\|done`, `find`/`get` (read-only, scoped to its own task) |
| Exploration budget | as much as needed to write a task body that needs none | zero — if it has to explore, the task body was insufficient |

*(Historical note: this repo was originally built and dogfooded with one
specific pairing — Claude Opus as orchestrator, Claude Sonnet as
implementer. The shape generalizes to any strong/cheap pairing, including
mixed-vendor ones; the table above is the definition, this note is not.)*

Only the orchestrator has git write access. Implementers edit files and run
tests inside their leased scope; landing the result (commit, PR, merge) is
always the orchestrator's job, whether it drives `work op=submit` itself or
reviews and commits an implementer's diff. The merge method is a merge
commit, never squash — the per-edge decision trail on the branch must
survive into `main` intact (see CONTRIBUTING.md for the policy and the
repository settings enforcing it). The edge commits themselves are
server-made; the orchestrator neither replicates nor batches them.

## Sequence of one task

```
orchestrator                    coord.db / git                 implementer (fresh)
     │                                │                                │
     │ draft kind=proposal            │                                │
     ├───────────────────────────────►│                                │
     │ (context pack: impact,         │                                │
     │  contracts, past rejections)   │                                │
     │                                │                                │
     │ user approves                  │                                │
     │ move to=approved               │                                │
     ├───────────────────────────────►│                                │
     │ draft kind=task (exhaustive     │                                │
     │  body: files, APIs, cmds,      │                                │
     │  constraints, scope)           │                                │
     ├───────────────────────────────►│                                │
     │                                │                                │
     │         spawn fresh subagent, hand it only the T-id             │
     ├──────────────────────────────────────────────────────────────► │
     │                                │  get <T-id>  (full brief)      │
     │                                │◄────────────────────────────── │
     │                                │  lease claim <paths> item=<id> │
     │                                │◄────────────────────────────── │
     │                                │  ── conflict? pick another ──  │
     │                                │     task, never wait idle      │
     │                                │  move to=active                │
     │                                │◄────────────────────────────── │
     │                                │  implement + test in scope     │
     │                                │  (no exploration — the brief   │
     │                                │   is the whole world)          │
     │                                │  move to=done                  │
     │                                │◄────────────────────────────── │
     │                                │  lease release <paths>         │
     │                                │◄────────────────────────────── │
     │                                │                                │
     │ check <id>                     │                                │
     ├───────────────────────────────►│                                │
     │ (drift? contracts binding?)    │                                │
     │ review diff, run gate/verify   │                                │
     │ git commit / PR                │                                │
     │ move to=archived               │                                │
     ├───────────────────────────────►│                                │
     │ (delta merges into spec.md)    │                                │
```

If the implementer instead finds the approach doesn't work, it does **not**
push through or improvise: `move to=rejected` with a note. That failure
joins the rejection corpus immediately — every sibling agent sees it on its
next `swarm` or `find scope=rejection` call, so the same dead end is never
retried blind.

## Anatomy of a thorough task body
Name the existing helpers the implementer must reuse — `find scope=code`
before writing; a redundancy finding at validation means the brief failed
to name a reusable helper, so fix the brief pattern, not just the code.

A task body is the implementer's *entire* context. If it has to explore to
fill a gap, the body was insufficient — that's an orchestrator bug, not an
implementer failure. Every task the orchestrator drafts (`draft kind=task`)
must contain:

- **Goal in one sentence** — what "done" looks like, unambiguously.
- **Exact file paths** — every file to change, and every new file to
  create, by full repo-relative path. No "find the relevant handler."
- **Relevant code excerpts or `go doc` commands** — either paste the
  signatures/structs that matter, or give the exact `go doc <pkg>.<Sym>`
  command to run to verify an API before using it. The implementer should
  never need to open a file just to learn a signature that could have been
  quoted.
- **Test expectations + verification commands** — which tests must pass,
  which new tests to add, and the exact command(s) to run (e.g.
  `go test -race ./internal/foo/...`, `make all`).
- **Scope boundaries** — what must *not* be touched, stated explicitly.
  This doubles as the `lease claim` path list.
- **Protocol steps** — a reminder of the lease → move active → implement →
  move done → lease release loop, so the implementer doesn't have to infer
  it from the server's general instructions.

Task bodies that meet this bar turn a cheap, context-free model into a
reliable implementer, because the hard part (deciding *what* to change and
*why*) already happened in the orchestrator's proposal review.

### How record bodies are written

Every item body — proposal, task, ADR, research — follows the same three
rules, which the server also states in its instructions manifest (MCP-007):

- **Compacted substance.** Constraints, decisions, measurements, rejected
  alternatives and why. Not a narrative of how the requirement arrived.
- **No verbatim quotes.** Never paste user quotes or transcript excerpts.
  They bloat every later read of the record without adding information —
  compact the input instead, losing nothing.
- **American English.** Spelling variants (behavior/behaviour,
  initialize/initialise) fragment full-text matches exactly like a language
  mix does, and `find`/`research` are FTS queries over these bodies.

## Fan-out

A single approved task is the unit of work, but the orchestrator rarely has
only one at a time. Once a batch of tasks is approved, it **partitions them
by disjoint scope** — the same declared paths that back each `lease claim`
are also the proof that two tasks don't collide — and **spawns one fresh
implementer per task, in parallel**, instead of running them one at a time.
Each implementer only ever sees and touches its own leased paths; the
orchestrator itself **serializes only the shared-file wiring** — the small
amount of glue (a registry entry, a router hookup, an import list) that
legitimately spans more than one task's scope and so cannot be leased apart.
This is what makes token cost scale with the number of approved tasks
instead of the size of the codebase: the orchestrator's context stays busy
briefing and reviewing, never blocked waiting on one implementer to finish
before starting the next.

## Importing a brownfield repo

Onboarding an existing repo follows a fixed six-step order, run once before
the normal loop starts:

1. **Index first.** `state/reindex` yields the code graph immediately and
   costs no decisions — it produces the real node IDs everything else
   anchors to.
2. **Survey in parallel.** Fan out read-only subagents over disjoint
   subtrees, one per top-level package or module; each reports the
   subtree's purpose, the invariants its code/tests/docs already assert,
   and candidate contracts. Read-only means no leases and no `work.md`
   contention, so this fan-out can go as wide as the tree — it is the same
   fan-out pattern as above, minus the scope collisions, because nothing is
   being written yet.
3. **Mint centrally.** The orchestrator turns the survey into `rule
   op=add` contracts, scoped per context dir and anchored via `applies` to
   the node IDs from step 1. Implementers never hand-write spec files; the
   server composes and lints them.
4. **Capture decisions.** Existing design docs and ADRs become `adr` items
   via `decide` (context, decision, consequences, status); pure reference
   docs stay plain `docs` items.
5. **Baseline.** Run `check` until it comes back clean — the stamped
   anchors are the point from which drift detection starts meaning
   anything.
6. **Then the normal loop** applies: `find scope=rejection`, draft, grill,
   implement.

Two guardrails keep this from drifting into busywork: encode only the
invariants the code, tests, and docs actually assert — never invented
ones — and start with the load-bearing few, letting `check`'s coverage
gaps show what is still unowned.

## Worktree isolation & who writes lifecycle state

Two rules keep a parallel fan-out from corrupting shared state:

1. **Each implementer runs in a dedicated git worktree**, never the shared
   main working tree — so no two implementers (nor the orchestrator) ever
   write the same source file, spec bundle, or `work.md` at once. The
   server's `work op=start` provides this: it re-roots the agent into a
   fresh worktree and semantic-replays that worktree's `.spectackle` state
   back on `submit`. A headless driver must replicate it — one worktree per
   implementer — rather than pointing every agent at the same root.

2. **The orchestrator owns every lifecycle `move`** (`draft → submitted →
   … → done → archived`). Implementers only claim and release their scope
   lease and edit code inside their worktree, then *report* completion —
   they never `move` items themselves.

The reason is a concurrency asymmetry. Item state lives in per-directory
`.spectackle/work.md`, a plain file: two processes doing read-modify-write
on it race (last writer wins) and silently drop item records. Scope leases
do **not** have this problem — they live in the shared `coord.db` (WAL,
cross-process safe). So leasing from an implementer is fine; moving items
from one is not. Keeping every `move` in the single orchestrator process
removes the item-record race by construction, and worktree isolation
removes it for code and spec files too.

## Decisions, grill & bounded feedback loops

Three additions close gaps in the loop above: research must happen before
any question reaches the user, plans get criticized before they're
delegated, and the implementer↔orchestrator retry loop is hard-capped so a
stuck task can't burn tokens forever. All three are server-mediated, not
LLM discipline.

### Research before you ask

Before the orchestrator puts a question to the user, it exhausts what the
server already knows: `research q=<topic>` returns one condensed pack —
impact, binding contracts, rejections, journal history, doc hits, and
structurally-generated gaps/open questions (`q` records) — at O(pack) cost,
not O(codebase). Only when that pack doesn't answer the question (external
knowledge, a measurement nothing in the repo can supply) does the
orchestrator mint a `research`-kind item (`R-xxxx`) with an exhaustive
brief and delegate it to a fresh, cheap subagent, exactly like any other
task — never ad hoc exploration in its own context, never a question the
pack could have answered.

### Grill before you approve

Before `move to=approved` on a proposal (or before delegating its child
tasks), the orchestrator runs `grill id=<P-id>`: a server-computed critique
pack — unanchored targets, contracts with no binding rule, child task
bodies that fail the exhaustiveness heuristic (`b` records, see "Anatomy of
a thorough task body" above), target packages with no tests, similar
rejections, and a checklist of open questions (`q` records). The critique
itself is the orchestrator's own reasoning; `grill` only supplies the
evidence. A successful grill stamps the item header `grilled: <date>` —
the proof a forward `move` checks for. Skipping it isn't fatal (`! GRILL W`
warning by default, tightenable to a hard block via `config.yaml`), but
it's the difference between a briefed implementer and one that has to
improvise.

### Decide: native UI, answer from anywhere

Every decision that actually needs the user goes through `decide`, never
unstructured chat. `decide op=ask` tries MCP elicitation (the same
native-UI mechanism `rule`'s slot forms already use) — radio for
enumerated choices, a confirm dialog for yes/no, free text otherwise. Two
outcomes:

- **The host renders it and the user answers immediately** — the decision
  is persisted (`ADR-xxxx` → `done`) and the orchestrator has its answer in
  the same call.
- **No elicitation support, declined, or a different harness entirely** —
  the `ADR-xxxx` item stays open; the orchestrator does **not** block on it.
  It keeps working other disjoint tasks and picks the answer up later —
  from `state`, from `swarm`'s sw piggyback, or because someone (any
  session, any harness) called `decide op=answer id=ADR-xxxx choose=…`. A
  decision made hours or days later, from an entirely different session,
  is a first-class re-entry, not a special case.

### Bounded feedback loops (rounds → blocked)

The implementer↔orchestrator retry loop has exactly two failure signals,
both server-counted — never the LLM's own bookkeeping:

```
implementer                    server                       orchestrator
    │  work op=submit              │                              │
    ├──────────────────────────────►  gate fails (verify/goal)    │
    │◄───────────────── rounds++ ──┤                              │
    │  fix, submit again           │                              │
    │                               │                              │
    │                               │◄──── move id=T-x to=active ─┤  (reopen from done)
    │                               ├───────────── rounds++ ──────►│
    │                               │                              │
    │                               │  rounds == max_rounds (default 3):
    │                               │  server side-steps the item
    │                               │    T-x -> blocked
    │                               │  and mints ADR-xxxx
    │                               │    (rescope | reject | override-once)
    │                               │    T-x needs: ADR-xxxx, sw escalate
    │                               │                              │
    │                               │◄──── decide op=answer ADR-xxxx │
    │                               │                              │
    │             rescope       -> draft     (mandatory rescoping)│
    │             reject        -> rejected  (note = decide reason)
    │             override-once -> active    (counter reset, ONCE)│
```

`blocked` is a side-state like `rejected` — outside the total order, never
visited on the happy path, and reachable/leaveable only through server
logic (`move`'s `to` enum never includes it — no tool call can set or clear
it directly). `next` and fanout skip `blocked` items structurally, the same
way they skip items with open `needs:`. The three exits are exhaustive:
`rescope` demands a smaller or different task before it can be re-approved,
`reject` closes it into the rejection corpus with the decide answer as the
note, and `override-once` resets the counter and hands it back to `active`
— but only once; a second escalation on the same item offers no override,
forcing an actual rescope or reject.

## Failure modes

- **Lease conflict** (`lease claim` returns `! LEASE` naming the holder) —
  never wait idle. Pick a different approved task with disjoint scope and
  come back to the blocked one later; if no other task is available, report
  the conflict up rather than polling.
- **Gate/test failure** — fix it and re-run the declared verify command
  until it's green. Never call `move to=done` on a red gate; a task marked
  done is a claim the orchestrator will trust without re-checking. Each
  gate failure also increments the item's server-counted `rounds` — see
  "Bounded feedback loops" above for what happens at the limit.
- **The task turns out to be wrong or unimplementable as written** — don't
  improvise around it. `move to=rejected` with a note explaining why. This
  is not a defeat: it feeds the rejection corpus, which siblings see in
  real time via `swarm`/`find scope=rejection`, and a rejection made with
  too little information is revocable (the orchestrator can move it back).

## This repo eats its own dog food

spectackle's own `.spectackle/` folders in this repository are driven exactly
this way: the orchestrator drafts and approves proposals and tasks, and
fresh implementer subagents on a cheaper model pick up approved tasks one
at a time through `lease`/`move`, with disjointness guaranteed by scope
leases rather than by coordination overhead. Self-hosting the workflow is
milestone 5 on the roadmap ([docs/roadmap.md](docs/roadmap.md)) — this
document describes the steady state that milestone converges on.

## Token economy: shell vs server

| Shell habit | Server tool | Why |
|---|---|---|
| grep/rg <symbol> | find q=<symbol> scope=code | Returns IDs + spans (O(results)), not file contents (O(codebase)) |
| grep -r over specs/history | find scope=rule\|rejection\|history | FTS5 over structured records, not O(files) text search |
| find(1) by filename | find scope=code | Signatures and file paths match too; ranked results |
| where is X defined / what calls X | get id=<node> depth=N | Cross-language call graph, not grep-inferred guesses |
| sed-style bulk edit | get depth first, then edit | Survey sites via get depth; sed is the edit, never the search |
| grep or sed .spectackle/ | find/get for reads, tools only for writes | Server-owned files: reads via API, writes via tools |

## Resident server self-restart (committed-only)

A dev build serving its own module may run with `-self-restart`: the server
polls `git rev-parse HEAD` and, when HEAD moves past the commit stamped into
the serving binary, rebuilds from a clean `git archive` snapshot of HEAD and
exec-replaces itself (same PID, same port, swarm identity carried across).
The dirty working tree is structurally excluded from every rebuild and
working-tree edits never trigger a swap (ADR-01KYF5): commit — or merge — to
ship a new serving generation. Foreign trees are refused by an eligibility
guard at serve start; non-git roots idle with one loud log line. The
mtime-based `make dev` hint is suppressed while the watcher runs, since its
advice would be a manual dirty-tree rebuild — the exact hazard this policy
closes. Swaps defer while any tool call or prompt render is in flight
(`Server.Busy`): a records-committing edge that moves HEAD completes on the
old generation, and the swap follows within one tick of quiet.
