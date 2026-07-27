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

## T-01KYGX9PB5F7HB7C9AZB5GET80 first outcome A/B: validation warn vs require on the limiter fixture measures the gates real catch rate
kind: task
state: draft
created: 2026-07-27
targets: docs/bench-curves.md, internal/bench/agent.go

ORCHESTRATOR-RUN benchmark task (the judges are the implementers; AGENT-ISOLATION-001 holds - each judge works its own temp fixture). P-01KYEVs tombstone left this as the named follow-up: the risk-gating break-even rests on an ESTIMATED 30-50 percent validation catch rate; the outcome fixture exists to replace the estimate with a number.

DESIGN, config-only variants on one binary (v0.2.0): side WARN preps the outcome fixture with feedback.validate warn (todays default flow - the judge archives without a forced verdict); side REQUIRE preps with feedback.validate require (the judge must render the validate pack and record a passing verdict under a second deliberate identity before archive). n=2 independent sonnet judges per side, 4 total, parallel, nonce-anchored. The scenario is unchanged (limiter brief, 5 hidden edge cases, 2 traps).

WHAT THE NUMBERS ANSWER: if REQUIRE shows rounds>0 with final-pass>first-pass where WARN does not, the gate CATCHES real defects and the catch rate replaces the estimate in the risk-gating rationale; if both sides land first-pass==final-pass at equal validity, require bought tokens without outcomes ON THIS FIXTURE SIZE and the ledger says so - either answer is a result. Efficiency renders per side (first-pass per 10K tokens); the comparison REFUSES if any run is invalid (all-valid rule).

MECHANICS: bench agent-prep -scenario outcome per workspace with the config overridden per side AFTER prep (prep writes the fixture config; the override edits feedback.validate before the judge starts and before the journal baseline... verify prep ordering - if the baseline snapshots after config the override is clean; state the ordering in the report). Judges get ONLY the brief file per the harness rules. Score with -nonces; aggregate with labels warn/require; append the batch to the docs/bench-curves.md ledger with the catch-rate conclusion.
HARNESS TOUCH (only if needed): if the prep API cannot inject the config variant cleanly, agent.go gains a -feedback prep option (additive); otherwise zero code changes.
VERIFY: the four agent reports pasted (calls/tokens/first/final/rounds/valid per run), the aggregate with efficiency lines, the ledger diff.
SCOPE: the run + ledger; agent.go only for the optional prep knob. No scenario changes, no scoring changes.
ROLLBACK: none needed for a measurement; the ledger entry is append-only history.
REPORT: the aggregate verbatim, the catch-rate conclusion in one sentence, prep-ordering statement, total token spend.
