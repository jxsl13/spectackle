---
schema: v1
---

## T-01KYM8W5TMFDM8WGY5KEQFV1WT swarm-contention A/B on v0.7.0: does enforcement replace the wasted conflict round
kind: task
state: draft
created: 2026-07-28
targets: docs/bench-curves.md

The measurement B-01KYKSKMHNE2H armed. Baseline (M-01KYKSKKPDFNT v1, v0.6.4): two concurrent judges on one target both opened worktrees, both landed (zero lost updates), but the slower one paid a full implement-then-git-conflict-resolve round. v0.7.0 enforces: the second start refuses naming the holder and its recoveries. HYPOTHESES: (1) both changes still land — enforcement must not cost correctness; (2) the loser now RECOVERS from the refusal text alone (waits for the holder submit, then starts) rather than implementing into a conflict; (3) zero git conflicts, against 1 in the baseline. PROTOCOL identical to the baseline for a clean A/B — only the binary version differs: same fixture recipe (one repo, two approved tasks both declaring shared.go, prep commits its own brief files this time per the recorded fixture lesson), two fresh sonnet judges with pinned identities judge-a/judge-b running CONCURRENTLY, neutral briefs telling them only to complete their task and follow refusal text. GROUND TRUTHS scored mechanically: both functions on main, both tasks terminal, empty lease table, and NEW - the count of git conflicts resolved (from the judge reports and the merge history) plus whether the refused judge recovered without help. RESULTS: swarm-contention v2 on the same key (delta against v1 journals automatically), metrics landed:count:+ lostupdates:count:- deadlocks:count:- plus new conflicts:count:-; impls judge-pair@v0.7.0; ledger row; new friction becomes bug items. VERIFY: the ground-truth checks quoted in the archive note; the v2 put renders its d-lines. SCOPE: measurement + records + one ledger row. ROLLBACK: none (records additive).
