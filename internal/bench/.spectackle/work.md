---
schema: v1
---

## P-01KYEVSK7BEEZBE7GGS4HSD5SV outcome-scored benchmarks: hidden acceptance tests in seeded fixtures make first-iteration completeness a measured number per token
kind: proposal
state: approved
created: 2026-07-26
refs: P-01KYESGDWFFMH80ENHNFXMVZE8, P-01KYD9466KEPWBV2RBK7EQM202
grilled: 2026-07-26
targets: internal/bench/bench.go, internal/bench/agent.go, docs/bench-curves.md

USER REQUIREMENT (2026-07-26): benchmarks must put token usage in relation to LLM RESULT quality. The mission is fully autonomous spec-driven development whose result is valid AND complete - no edge cases or bugs surviving the first iteration - inside the mechanical git workflow, with the LLM spending tokens only on judgment work: state traversal, task elaboration, code, research, validation, refutation. Writing and running tests during implementation is allowed LLM work; composing git/forge commands never is.

WHAT THE HARNESS MEASURES TODAY, and the gap: fixture v3 plus the judge-agent stage meter tool calls and bytes with validity gates and tamper evidence (nonce anchors, journal write deltas), and the reference curves in docs/bench-curves.md hold cost per scenario. Nothing measures whether the PRODUCED ARTIFACT is any good: a variant that halves tokens by producing a shallower implementation currently wins the A/B. Cost without outcome optimizes the wrong axis.

DESIGN - deterministic outcome scoring, no judge subjectivity where avoidable:
1. PLANTED-COMPLETENESS FIXTURES. Each new fixture carries a task brief enumerating a feature whose correct implementation must survive N edge cases, plus HIDDEN acceptance tests - not visible to the implementer agent, applied by the harness after the run - one per planted edge case (and at least one planted trap: a vacuous-test temptation, an offscope-file temptation). The brief states the feature; the hidden tests define complete.
2. SCORE. Per run: tokens_total (existing meter), first_iteration_pass_rate (hidden tests green after the FIRST done, before any reopen), final_pass_rate, reopen_rounds, review_findings_addressed (from the verdict machinery once the approved chain lands). Efficiency = first-iteration completeness per 10K tokens at equal validity; the A/B verdict refuses to compare runs whose validity gates differ, mirroring the existing all-valid rule.
3. WHAT IT CALIBRATES. The review chains risk-gating break-even (feedback.validate=require threshold) currently rests on ESTIMATED catch rates (30-50 percent); these fixtures measure the real catch rate of grill/validate per variant, and the review-mode economics (single-reviewer sequential lenses vs opt-in panels) become an A/B on the same fixtures instead of an argument.
4. ROLE-BOUNDARY CONTRACT. One EARS rule pinned at the root, composed via rule op=add: the server SHALL perform every mechanical workflow step (branch, commit, push, PR, merge, CI await) itself, and no tool result SHALL instruct the caller to run a git or forge command. The bench asserts it: a scenario transcript containing a git instruction to the agent is a validity failure.
5. TOKEN BOUNDS. Hidden tests live outside the fixture workspace (harness-held), cost zero agent tokens; scoring is harness code; one new bench flag -outcome selects the fixture set; curves table gains three columns (first-pass, final-pass, rounds).

EXIT CRITERION on this repository: one outcome fixture with >=5 planted edge cases and 2 traps runs end to end under the judge harness; the report renders tokens, first-iteration pass rate, final pass rate, rounds; an A/B between two hint variants renders efficiency at equal validity and refuses the comparison when validity differs; the role-boundary rule exists, is anchored, and its bench assertion fails a transcript that tells the agent to run git.
