---
schema: v1
---

## T-01KYKPPZH0E1ABCBC02N8PXT5E worktree-scenario judge batch on v0.6.3: the swarm flow measured with independent agents
kind: task
state: draft
created: 2026-07-28
grilled: 2026-07-28 open=0
targets: docs/bench-curves.md

Scenario-coverage completion: tricky (M-01KYKEWKMEEWA 3/3) and outcome (M-01KYKMNFN2EQK v3 3/3) are batch-covered; the WORKTREE scenario (the dedicated swarm flow: work op=start leasing item+targets, implementation inside the worktree root, work op=submit gate-integrate-merge-replay) has never run as its own batch - outcome judges exercised it incidentally. n=3 fresh sonnet judges on v0.6.3, bench -agent-prep DIR -scenario worktree, positional nonces, bench -agent-score per workspace. HYPOTHESES: (1) the submit flow (gate, integrate main, re-gate, ff-merge, replay) navigates from renders alone - the flow=true goal column; (2) the v0.5.3 SPECTACKLE_AGENT discoverability fixes hold in the scenario BUILT around identity (zero lost retry loops); (3) the diet renders (v0.6.0) cost no navigation validity - worktree edges collapse like lifecycle edges. RESULTS: new record name=worktree-navigation, all-any frame, metrics navigated:count:+ valid:count:+, impls judge-batch@v0.6.3; per-run calls/tokens, every trap/friction in the note; new friction becomes bug items; ledger row citing the M- id. VERIFY: three agent-score outputs quoted in the archive note; the put renders. SCOPE: measurement + records + one ledger row. ROLLBACK: none (records additive).
