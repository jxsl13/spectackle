---
schema: v1
---

## T-01KYH1GK27F8PAY8BWC7FNJZ2S catch-rate rerun at n=3 per side: warn vs require on the fixed meter, the first real validation catch rate
kind: task
state: draft
created: 2026-07-27
targets: docs/bench-curves.md

ORCHESTRATOR-RUN benchmark, the follow-up B-01KYGZNTs archive note assigned: the first A/B (T-01KYGX9P) yielded qualitative findings but the disqualified run blocked a rate; the meter now sanitizes argv and locks appends, so a full batch can score valid. DESIGN unchanged from T-01KYGX9P (config-only warn vs require on the current binary, outcome fixture, sonnet judges, nonce anchors) at n=3 per side, 6 judges parallel. NUMBERS SOUGHT: per-side first-pass/final-pass/rounds/tokens; the efficiency lines at all-valid; the CATCH RATE stated as repaired-runs over require-runs with the repair magnitude (hidden cases gained via gate rounds), and the semantic-miss count as its complement - replacing the estimated 30-50 percent band with the measured figure in the risk-gating rationale paragraph of the ledger. INTERPRETATION DISCIPLINE: a run is REPAIRED only if final>first AND rounds>0; first==final under require counts as no-catch regardless of rounds; disqualifications this time are harness bugs to file, not shrugs. VERIFY: six agent reports pasted, the aggregate with efficiency, the ledger diff replacing the estimate. SCOPE: the run + ledger only; zero code changes expected. ROLLBACK: none (measurement). REPORT: the rate, the per-side medians, total spend.
