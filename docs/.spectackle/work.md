---
schema: v1
---

## T-01KYKS8D9KEWN84MB9A10WKSDK swarm-contention benchmark: two concurrent judges collide on one lease and must both land
kind: task
state: draft
created: 2026-07-28
targets: docs/bench-curves.md

The multi-agent core (leases, l-line refusals, holder naming, release-on-submit) has never been stressed by INDEPENDENT concurrent agents - every batch so far ran judges in isolated repos. Custom fixture (no prep scenario exists): one git repo with shared.go (contended) and two pre-seeded APPROVED tasks, T-A appending FnA to shared.go and T-B appending FnB - same declared target, so work op=start auto-claims collide. Two fresh sonnet judges run CONCURRENTLY, each with a fixed persistent identity (brief instructs prefixing every call with SPECTACKLE_AGENT=judge-a / judge-b) and a neutral self-contained brief-a.md / brief-b.md: complete your task via the CLI; if scope is held by a sibling, the renders tell you what to do. GROUND TRUTHS scored mechanically after both finish: (1) both-landed - main contains FnA AND FnB (no lost update); (2) no-deadlock - both tasks reach done/archived; (3) serialized - git history shows two distinct submit merges; (4) clean-exit - lease op=ls empty at the end; the loser transcript evidence (l-line naming the holder, successful retry) comes from the judge reports. HYPOTHESES: (a) the l refusal names the holder well enough that the losing judge waits-and-retries without a lost round; (b) release-on-submit is prompt enough that the retry succeeds without TTL expiry; (c) no interleaving corrupts shared.go or the records. RESULTS: record name=swarm-contention, all-any frame, metrics landed:count:+ lostupdates:count:- deadlocks:count:-, impls judge-pair@v0.6.4; ledger row; new friction becomes bug items. VERIFY: the mechanical ground-truth checks quoted in the archive note; the record put renders. SCOPE: measurement + records + one ledger row. ROLLBACK: none (records additive).
