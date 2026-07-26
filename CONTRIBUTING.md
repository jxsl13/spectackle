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

## Every change lands through a pull request that merges on green CI — with a merge commit, never a squash

Nothing is pushed straight to `main`. Work goes onto a branch, opens a pull
request, and merges the moment CI is green, with no one waiting on a button.
A red CI simply means it never merges.

The merge method is a **merge commit — never squash**. Every state-machine
edge and every distinct change is its own commit on the branch, and each
message carries the decision it records; a squash is a lossy compression of
that decision log, collapsing N recorded decisions into one blob on `main`
and destroying exactly the trail the branch built. This policy stands on its
own: it holds whether or not the server's per-edge commit machinery is
active for a given workspace.

The branch hygiene consequence: because a merge commit preserves every
branch commit on `main` forever, branches carry clean per-edge and
per-change commits, not fixup noise. Work-in-progress commits are amended or
rebased **before** the pull request opens — never cleaned up by squashing at
merge time, which is the exact operation this policy forbids.

Three repository settings make the policy real, and without them the intent
is silently a no-op:

1. **Settings → General → Pull Requests → Allow auto-merge.** Without it,
   arming auto-merge is refused outright.
2. **A branch protection rule (or ruleset) on `main` requiring the CI check.**
   This is the part that is easy to miss: with no required check, GitHub
   considers a fresh pull request immediately mergeable and there is nothing
   for auto-merge to wait on — "merge when CI passes" then degrades to
   "merge now, CI or not".
3. **Settings → General → Pull Requests → Allow merge commits ON, Allow
   squash merging OFF.** This is the hard enforcement, and it is a one-time
   manual step for the repository owner — nothing inside the repository can
   flip it. With squash left enabled, one habitual click on the wrong merge
   button silently flattens a branch's decision trail, and nothing fails
   loudly: the loss is only discovered when someone goes looking for a
   decision that `main` no longer carries.

A merged pull request is finished and is never reused. Follow-up work
restarts the branch from the current `main` and opens a new pull request;
stacking commits onto already-merged history is what produces the "why is
this diff enormous" pull requests nobody can review.

## The resident server must be rebuilt and restarted after every merge

spectackle develops itself with itself: the resident MCP server *is* the
product under change, not a bystander to it. Every merged feature or fix
must be followed by `make dev` — it rebuilds the binary and restarts the
resident server, proving readiness with a real `state` call before
returning. Skipping this leaves a stale binary answering `find`/`get`/`draft`
calls from code that no longer exists, which looks and feels exactly like a
new defect in whatever you just shipped, not like an operator error. `make
dev` is idempotent (safe to run again if unsure) and is the natural next
step after `make all` goes green. See the [README resident-service
section](README.md#resident-service-recommended-for-more-than-one-call) for
`dev-stop`/`dev-status` and the manual invocation it wraps.

## `.spectackle/` is server-written only

Never hand-edit files under `.spectackle/` — they are the server's
coordination and spec state (leases, journal, cache, specs) and are written
exclusively through MCP tool calls (`draft`, `move`, `rule`, `check`,
`compact`, ...). Hand edits will desync from the journal and get overwritten
or rejected by drift detection.
