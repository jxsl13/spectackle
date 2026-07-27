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

## T-01KYH6H9SYFZNSW666Z1NRE24Z ask-oracle benchmark: judges who ask about unstated semantics get answers, measuring whether elicitation converts semantic misses into first-pass completeness
kind: task
state: draft
created: 2026-07-27
targets: internal/bench/outcome.go, internal/bench/agent.go, docs/bench-curves.md

ORCHESTRATOR-RUN benchmark evolution. MOTIVATION, measured: across both catch-rate batches the dominant residual defect was SEMANTIC - four independent judges hardened Allow(n<=0) into a success-no-op against the hidden refusal semantics, and no gate round touched it, because computed classes see shapes, not meaning. The workflows designed answer to unstated semantics is elicitation (P-01KYES: ask, never guess) - but benchmark judges have never been ABLE to ask (the harness says nobody answers). This run measures elicitations value directly.

DESIGN: scenario variant outcome-ask. (1) BRIEF DELTA, one sentence added to outcomeBrief for this variant only: unresolved behavioral questions MAY be asked via decide op=ask - they will be answered; guessing unstated semantics is at your own risk. (2) ORACLE: the orchestrator (not new Go code) watches each judge workspace during the run - poll decide op=ls / state for open ADR items every ~45s, answer via decide op=answer from a hidden FAQ held OUTSIDE the workspaces: n<=0 semantics (Allow(0) true no-op, negative false no-op), over-capacity false, refill fractional clamped, clock injectable, concurrency no-oversell; unmatched questions get the scripted answer unspecified - your call, document the choice in the task body. (3) SCORING: AgentScore gains AskCount (decide-ask calls counted from the meter argv, exit-0 decide calls containing op ask) rendered on the outcome line - additive field, journal untouched. (4) RUN: n=3 judges, require config (the full chain), fixed-shim infrastructure; comparator is the pooled require data from batches 1-2 (0 asks by construction, 4/5 modal first-pass, semantic miss in 4 of 5 contents).

NUMBERS SOUGHT: ask-rate (judges asking at all), the asked-vs-guessed split on the n<=0 case specifically, first-pass delta against the comparator, token cost of the ask round-trips. HYPOTHESES STATED AHEAD: H1 asks convert the n<=0 miss into a pass; H0 judges do not ask despite the invitation (also a result - the invitation text failed, EVOLUTION-001 labels it measured-and-negative).
NON-NEGOTIABLE: AskCount unit test over a synthetic meter log; the brief delta test-pinned in the honest-map test family; the oracle FAQ recorded verbatim in the ledger entry so the answers are auditable; interpretation rules pinned BEFORE data (this brief is the pin).
VERIFY: build/test/vet/gofmt; lint; check ok; three judge reports, the ask ledger, first-pass table vs comparator.
SCOPE: outcome.go (brief variant + scenario plumbing), agent.go (AskCount), the run, the ledger. ROLLBACK: revert; the variant is additive.
REPORT: ask-rate, the n<=0 outcomes per judge, the delta, total spend.
