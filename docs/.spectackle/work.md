---
schema: v1
---

## T-01KYKM21QFF3KAM7MRY30EYBPJ outcome judge batch on v0.6.2: does the in-loop vacuous-test finding move validity off 1/3
kind: task
state: draft
created: 2026-07-28
targets: docs/bench-curves.md

The measurement T-01KYKGZT0S armed: both prior outcome batches (M-01KYJWG08TFRC v1 baseline, M-01KYK5ETQ2F22 v2 on v0.5.2) voided 2 of 3 judges on the vacuous-test trap - assertion-free smoke tests invisible until post-hoc scoring. v0.6.2 surfaces them at check as ! VAC W findings over dirty test files, and the outcome brief drives check until ok, so the hypothesis is direct: (1) judges hitting the VAC finding fix their tests before done and validity rises above 1/3; (2) the finding does not add asks or reopen rounds (the fix is a local edit); (3) the bait-bug goal miss (j1 in the v2 batch also missed it) is INDEPENDENT of this change - track it separately in the note. Protocol identical to T-01KYK4876N: n=3 fresh sonnet judges, bench -agent-prep DIR -scenario outcome on the v0.6.2 binary, positional nonces, bench -agent-score per workspace. RESULTS: outcome-navigation v3 (same all-any key as v1/v2 - the put delta against v2 journals automatically), metrics inherited (navigated:count:+ valid:count:+), impls judge-batch@v0.6.2, note carrying per-run calls/tokens, every VAC finding hit and whether the judge acted on it, and the bait-bug outcome; ledger row citing the record; new friction becomes bug items. VERIFY: three agent-score outputs quoted in the archive note; the v3 put renders its d-lines against v2. SCOPE: measurement + records + one ledger row. ROLLBACK: none (records additive).
