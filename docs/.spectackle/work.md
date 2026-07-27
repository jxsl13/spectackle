---
schema: v1
---

## T-01KYJ58DBAFD798A6HNDWDKAPK outcome judges on the commit-only offline surface: does the new vocabulary navigate as completely as the PR theater it replaced
kind: task
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: docs/bench-curves.md

ORCHESTRATOR-RUN benchmark (AGENT-ISOLATION-001: judges are fresh-context implementers driving the real binary through the metering shim; the orchestrator only preps, spawns, and scores). QUESTION: the offline collapse (T-01KYHAH1GJ) replaced the branch/draft/ready/merged lines with g offline commit renders, GATE E fix-and-retry refusals, and records lines - the scripted A/B measured -198 tokens per lifecycle, but scripted steps cannot measure NAVIGATION: whether a fresh agent still finds its way through the state machine first-iteration-complete on the new vocabulary. METHOD: n=3 judges, scenario=outcome (hidden acceptance tests score first-iteration completeness per token), v0.3.1 binary, prep via bench -agent-prep -scenario outcome, score via bench -agent-score with positional nonces (DISQUALIFIED on sequence break or forged rewrite). BASELINES for comparison (docs/bench-curves.md ledger): the pre-collapse outcome batches T-01KYGX9P (warn arm) and T-01KYH1GK (n=3 rerun, catch rate 25 percent structural-only) - completeness and token figures per judge. INTERPRETATION RULES pinned BEFORE data (BENCH-INTERP): all-valid gate applies (a DISQUALIFIED or INVALID judge voids its slot, rerun); completeness is the score tools verdict, never the judges self-report; a completeness drop at equal-or-lower tokens reads as a navigation regression of the NEW surface (hints insufficient), a completeness hold at lower tokens confirms the collapse end-to-end; single-batch n=3 bounds confidence - the ledger entry says so. DELIVERABLE: ledger entry with per-judge tokens/completeness, comparison against both baselines, and any judge-reported friction on the new lines verbatim-compacted into findings; new bugs found by judges get filed as B-items. VERIFY: the score tool exit codes plus the ledger diff. SCOPE: benchmark run + docs/bench-curves.md append; no server code. ROLLBACK: revert the docs commit.
