---
schema: v1
---

## T-01KYKEJF29EVJVSMJVT3MDAWFK tricky-scenario judge batch on v0.6.0: blocked, decide and reopen states measured with independent agents
kind: task
state: active
created: 2026-07-28
grilled: 2026-07-28 open=0
targets: docs/bench-curves.md

All-states coverage per the standing mandate: the outcome batches (M-01KYJWG08TFRC v1/v2) covered draft-active-done-archived plus the reject path; the TRICKY scenario covers what no batch has - rule slot authoring, the reopen loop escalating into BLOCKED, and the decide exit - on the v0.6.0 binary. Protocol mirrors T-01KYK4876N: n=3 fresh sonnet judges, bench -agent-prep DIR -scenario tricky, positional nonces, independent fresh-context agents execute the prepped briefs, bench -agent-score DIR -nonces <nonce>. HYPOTHESES: (1) judges navigate the blocked-state escalation and its decide exit from renders alone (no asks about the mechanism); (2) rule slot need records are answerable by the agent without user round-trips (ELICIT-001 surface); (3) the violation-honest verdicts (v0.5.2) hold on a scenario with a different trap profile. RESULTS: new bench record name=tricky-navigation, all-any frame, metrics navigated:count:+ valid:count:+, impls judge-batch@v0.6.0; per-run calls/tokens and every trap or friction in the note; new friction becomes bug items; ledger row citing the M- id. VERIFY: three agent-score outputs quoted in the archive note; the record put renders; go build ./... green (no code changes expected). SCOPE: measurement + records + one ledger row. ROLLBACK: none needed (records additive).
