---
schema: v1
---

## ADR-01KYHPNG8PF41B64KTBFS0ZH0A Offline single-branch: does the zero-branches rule apply to the swarm worktree flow too? Worktrees structurally require a transient local branch (never pushed, no PR); killing them offline removes parallel implementers there. Lifecycle edges (start/done/archive) become commit-only either way.
kind: adr
state: done
created: 2026-07-27
decision: scope zero-branches to lifecycle edges; worktree flow stays (transient local branches allowed)
consequences: mode: offline collapses the gitFlowFor edges to commit-only on the current branch (zero branches, zero PRs, zero pushes, zero base checkouts there); the swarm worktree flow is exempt BY NAME - its transient local branches and local merges are already push- and forge-free and stay byte-identical in both modes. The submit-by-commit clause is dropped from the task; linearity assertions scope to the worktree-less lifecycle.
status: accepted

kind: radio
option: scope zero-branches to lifecycle edges; worktree flow stays (transient local branches allowed)
option: offline forbids worktrees entirely - single branch means single branch
option: decide later - implement the lifecycle collapse first
blocks: T-01KYHAH1GJEFZ861R0NGT9W8PV
choice: scope zero-branches to lifecycle edges; worktree flow stays (transient local branches allowed)
