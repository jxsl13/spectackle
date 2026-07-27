---
schema: v1
---

## ADR-01KYGCJ70JESXTEGZJVWF7AXN4 v0.2.0: the full chain landed early - cut the release now or hold to the planned Aug 1-2 window?
kind: adr
state: done
created: 2026-07-26
decision: cut now
status: accepted

kind: radio
option: cut now
option: hold to Aug 1-2
choice: cut now

## T-01KYHAH1GJEFZ861R0NGT9W8PV offline mode is single-branch commits only: no PRs, no pushes, no branch dance
kind: task
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/gitflow.go, internal/forge/offline.go, docs/lifecycle.md

USER REQUIREMENT (two directives, 2026-07-27): (1) offline mode creates NO PRs and pushes NOTHING - lifecycle works commit-only on the current branch; (2) offline is the DEFAULT - online requires the explicit git: mode: online key (GIT-DEFAULT-001). SCOPE DECISION (ADR-01KYHPNG8P, user-chosen): zero-branches/zero-PRs/zero-pushes/zero-base-checkouts applies to the gitFlowFor EDGES; the swarm worktree flow is exempt BY NAME - its transient local branches and local merges are already push- and forge-free and stay byte-identical in both modes; the submit-by-commit idea is dropped (squash violates never-squash ADR-01KYDB, cherry-pick rewrites ledger SHAs); linearity assertions scope to the worktree-less lifecycle.

DESIGN (grill round 2, two independent judges, recon-verified line numbers): EDGES under offline take an early mode split per edge, online text byte-identical: start = CommitCode + gitCommitRecords on the current branch rendering g offline commit <short-sha> <subject> per commit (g offline clean when nothing), refusing detached/unborn HEAD with a ! GIT E line (the EnsureBranch checkout normalizer at gitflow.go:205 dies with the branch dance); sync = same minus the HasUnpushedCommits/push block (:323-339 collapse to one g committed line); ready = sync + runGate verdict (red gate: item stays done); merge = (runGate iff the item forward-skipped dones gate, today behind the draft-flip :573-579) + gitCommitRecords + closureComplete=true ON RECORDS-COMMIT SUCCESS and false on failure - CRITICAL: flowAttemptedMerge (tools.go:1701-1708) matches the records substring, so without this every offline archive is compensated back to done forever; flowAttemptedMerge itself stays untouched and the tools.go:1681 compensation keeps firing on genuine records failures. STRUCTURE: forgeFor loses its offline arm (mode: offline never constructs a forge; NewOfflinePersistent loses its production caller, kept as the test double); gitPush structurally unreachable offline (keep :811 guard as tripwire); no checkout/restore-defer/reconcileClosureBranch/branch logic reachable offline - gitFlowMerge (:451-598) collapses to records+closureComplete. DEFAULT FLIP: GitCfg mode parsing defaults to offline when the key is absent; mode: online opt-in; enabled: false master-off unchanged; document the breaking change (repos relying on implicit online must add the key).

KNOWN LOSSES, said not hidden: the offline merge no longer lands closure records on the base branch (records live on whatever branch is current - lifecycle.md notes it); B-01KYEEJKQs detached-head self-heal (the archive merge lands it, swarm.go:889-893 + swarm_test.go:966 wording) dies - message becomes an offline-true statement; legacy offline repos hold parked spectackle/<id> branches and cache/forge-offline.json PRs that nothing will merge - documented as inert; ORCH-SYNC-001s wait-for-merged-line ritual is online-only - orchestrator docs say so.

PHASED LANDING (one branch, one PR, every commit green): P1 seam - unexported Server forge override honored by forgeFor + local bare-repo remote fixture (git init --bare + remote add); port the 9 branch-dance tests (swarm_test.go:827,904; gitflow_test.go:450,525,564,583; closurefix_test.go:34,69,115) to mode: online + injected offline forge, proving the online shape is pinned independently BEFORE anything changes. P2 bench dual-surface - recordFamilies (bench.go:82-86) accepts old OR new lines temporarily (bench is production code; never-silent scoring would fail every run mid-migration). P3 the collapse, atomic - offline early-paths in all four edges + closureComplete semantics + forgeFor arm deletion + the DEFAULT FLIP + migrate gitflow_test.go:411 (invariant survives: closure lands mechanically - evidence lines change), swarm_test.go:966 wording, rolebound_test.go:35 recalibration (negative control tests the surface that exists), shimlock :174 merged-line assert, bench families final flip - same commit. P4 pins - remote-less fixture e2e asserting unchanged git branch --list, no offline:// anywhere, empty rev-list --merges (worktree-less), zero push invocations via a PATH-shim git spy; offline atomicity twin of closurefix:115 (records-commit failure refuses whole and compensates); default-flip tests (no mode key -> offline behavior; mode: online -> forge constructed). P5 docs - lifecycle.md offline paragraph + accepted-loss + legacy note, tools.md:203-205 online-scoped, gitflow.go/offline.go headers document the mode-vs-test-double split, workSubmit detached-head message. C-class tests (19 offline fixtures incl. wipeguard family, compactsweep, gitflow:122,153,478) keep their fixtures - the sweep depends on P3s closureComplete fix. DIFF ESTIMATE (judge): ~12-15 files, ~650-850 lines total.

VERIFY: go build ./... && go test ./... -count=1 && gofmt -l . empty at EVERY phase commit; final: the offline lifecycle commit log pasted, the per-test migration outcome table pasted, full online suite green (byte-identity control group: worktree_e2e/invariants no-remote online fixtures). SCOPE: gitflow.go, tools.go comments, swarm.go wording, forge/offline.go header, config parsing, bench.go+agent.go, the named tests, docs. NON-GOALS: forge interface unchanged; swarm flow unchanged; no new config keys beyond honoring mode: online. ROLLBACK: revert the PR. REPORT: anything the single-branch model makes impossible - say it, do not approximate.

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
