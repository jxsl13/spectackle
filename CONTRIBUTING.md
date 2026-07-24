# Contributing

spectackle is developed with the same swarm loop it implements. Before
touching anything, read [docs/agent-workflow.md](docs/agent-workflow.md) —
it defines the two roles and is the full spec for what follows here.

## The two roles

- **Orchestrator** — persistent context, strong model, git rights (commits,
  PRs, merges). Drafts proposals, writes exhaustive task bodies, reviews
  implementer output, runs `check`/gate, archives.
- **Implementer** — fresh, minimal-context, cheap model, never commits or
  pushes. Pulls one approved task via `lease`/`get`, implements it inside
  its leased scope, hands it back via `move`.

Never mix the two: an implementer that has to explore beyond its task body
means the task body was insufficient, not that it should improvise. The
orchestrator writes the brief so the implementer never has to explore —
that keeps the orchestrator's own context free of implementation noise, and
it makes token cost scale with the number of tasks, not the size of the
codebase.

## Every change starts with `find` + `draft`

Before drafting anything, run `find scope=rejection` (and `swarm`) for the
area you're about to touch — a sibling may have already tried and failed
there; don't repeat a dead end blind. Every change then begins as
`draft` (proposal, then task) and needs explicit approval before
implementation starts. See the [README loop](README.md#orchestrated-swarm-workflow-cheap-fresh-subagents)
for the end-to-end sequence.

## Lifecycle moves are forward-skip

States are totally ordered
(`draft < submitted < approved < active < done < archived`); any forward
jump is legal in a single `move` call (e.g. `draft` straight to `active`),
and skipping to `archived` implies `done`. `rejected` requires a note and is
revocable back to any pre-`done` state. Full rules:
[docs/lifecycle.md](docs/lifecycle.md).

## `make all` must be green before any PR

`make all` runs build, vet, test, spec lint, smoke, and the coverage gate
(`cover`, ≥70%). It must pass locally
before you open a PR — CI ([.github/workflows/ci.yml](.github/workflows/ci.yml))
re-runs the same steps plus `make fuzz` and the self-hosting `check` gate, and does not
merge red.

## `.spectackle/` is server-written only

Never hand-edit files under `.spectackle/` — they are the server's
coordination and spec state (leases, journal, cache, specs) and are written
exclusively through MCP tool calls (`draft`, `move`, `rule`, `check`,
`compact`, ...). Hand edits will desync from the journal and get overwritten
or rejected by drift detection.
