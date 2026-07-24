# Agent workflow — orchestrator + fresh implementers

This document specifies the swarm shape spectacle is built for, and the one
this repo itself is developed with (self-hosting, see
[docs/roadmap.md](docs/roadmap.md) milestone 5). It is a product concept,
not just a dev note: the server's tool grammar, lease system, and
instructions text (`internal/mcpserver`) exist to make this division of
labor cheap and safe.

The core bet: **exploration is the most expensive part of agentic coding.**
A model that has to grep the tree, read five files to find the right API,
and reconstruct constraints from scratch burns far more tokens than the
edit itself costs. spectacle attacks this from both ends — `find`/`get`/
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
reviews and commits an implementer's diff.

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
  is persisted (`D-xxxx` → `done`) and the orchestrator has its answer in
  the same call.
- **No elicitation support, declined, or a different harness entirely** —
  the `D-xxxx` item stays open; the orchestrator does **not** block on it.
  It keeps working other disjoint tasks and picks the answer up later —
  from `state`, from `swarm`'s sw piggyback, or because someone (any
  session, any harness) called `decide op=answer id=D-xxxx choose=…`. A
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
    │                               │  and mints D-xxxx
    │                               │    (rescope | reject | override-once)
    │                               │    T-x needs: D-xxxx, sw escalate
    │                               │                              │
    │                               │◄──── decide op=answer D-xxxx │
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

spectacle's own `.spectacle/` folders in this repository are driven exactly
this way: the orchestrator drafts and approves proposals and tasks, and
fresh implementer subagents on a cheaper model pick up approved tasks one
at a time through `lease`/`move`, with disjointness guaranteed by scope
leases rather than by coordination overhead. Self-hosting the workflow is
milestone 5 on the roadmap ([docs/roadmap.md](docs/roadmap.md)) — this
document describes the steady state that milestone converges on.
