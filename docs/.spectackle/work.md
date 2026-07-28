---
schema: v1
---

## T-01KYK4876NF2MBZQTMBFRFRN9Y outcome judge batch on v0.5.2: A/B the judge-facing hint fixes against the 2026-07-27 baseline
kind: task
state: done
created: 2026-07-28
grilled: 2026-07-28 open=0
targets: docs/bench-curves.md

The measurement cycle for the v0.5.2 judge-facing fixes (B-01KYJ66VSQ start-hint identity binding, B-01KYJ67RF9 violation-honest verdicts), mirroring the T-01KYJ58DBA protocol: n=3 fresh independent judges, scenario=outcome, positional nonces, v0.5.2 binary. Each judge: bench -agent-prep DIR -scenario outcome, an independent fresh-context agent (sonnet) executes the prepped brief in DIR, bench -agent-score DIR -nonces <nonce> scores goals+bytes+violations. HYPOTHESES under test: (1) the SPECTACKLE_AGENT retry loop (3/3 judges in the baseline batch) is gone for hint readers; (2) violation-voided runs now render their cause (no hand forensics); (3) validity improves from 1/3 (baseline caveat: vacuous-test trap voided 2). RESULTS: put as a new version of the outcome-navigation bench record (M-01KYJWG08TFRC key: name=outcome-navigation all-any frame) with metrics navigated:count:+ valid:count:+ and note carrying binary version, per-run calls/tokens and any traps hit; the put delta against v1 journals automatically. docs/bench-curves.md ledger gains the batch row citing the record; new judge-found friction becomes bug items. VERIFY: three agent-score outputs quoted in the archive note with their verdict lines; the record put renders its d-lines; go build ./... green (no code changes expected). SCOPE: measurement + records + one ledger row. ROLLBACK: rm the record version (bench op=rm is NOT needed - history keeps v1).
