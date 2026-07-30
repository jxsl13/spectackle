---
schema: v1
---

## T-01KYSPFXHNFZ7R35H9ADD09R8Z give the judge harness the real tool descriptions so the 20KB schema surface has a measurable benefit side
kind: task
state: draft
created: 2026-07-30
targets: internal/bench/agent.go

The cost half of B-01KYS711ZFFG0 is done: bench now meters the tools/list payload over the wire and reports schema, session and their deltas. Measured, this workspace pays 24322B before a single tool call - 20023B of tools/list plus 4299B of manifest - against a scripted per-call total of 3039B. So the schema surface alone is 6.6 times the entire scripted run, and the manifest line that had presented itself as THE once-per-session cost is 18 percent of the real one.

WHAT IS STILL UNMEASURABLE, and why this task exists. The judge harness deliberately hands the agent tool NAMES ONLY. So the 20KB of descriptions and input schemas now has a measured COST and still has no measured BENEFIT: nothing shows whether a description earns its bytes by preventing a wrong call, or whether it is dead weight the caller never reads. Ranking a schema trim needs both sides. Until then any tool-description edit is unmeasured by construction, and BENCH-001 - revert a surface change whose judged metric did not improve - cannot adjudicate it, because the judged metric cannot see it.

WHY THIS IS NOT A ONE-LINE CHANGE, and the reason it was split out rather than folded in. Handing the judge the real descriptions changes what the harness IS. Today the name-only brief measures how well the workflow reads from the manifest and the record grammar alone; with descriptions it measures something else, and the existing judge reference curves in docs/bench-curves.md were all produced under the old regime. So the two modes have to coexist and be labeled, or every historical batch becomes incomparable - which would destroy the only longitudinal data this repository has about its own surface.

DIRECTION. Add a mode to agent-prep - alongside the existing -with-manifest - that injects the real tools/list payload into the brief and records its size in a sidecar the way manifest.size already works. Keep name-only the default so history stays comparable, and make the scored output state which mode produced it, because a batch whose mode is unlabeled is worse than no batch. Then the first honest experiment becomes possible: run n=3 batches in both modes and compare goal attainment per token. That is the measurement that tells whether 20KB of descriptions buys anything at all, and it is the precondition for trimming them.

STATED TRAP, from the cost work: two of the fattest metered lines are landed judge FIXES - a 246B VALIDATE W advisory and a 58B gloss on the ROUNDS refusal - which exist because judges misread the shorter versions. A byte metric alone scores removing them as a win. That is BENCH-001 inverted, the benchmark sanctioning a regression, and it is exactly why the benefit side has to exist before any trimming is justified by numbers.

VERIFY. A prep run in the new mode whose brief contains a tool description verbatim and whose sidecar records the payload size; a scored run that names its mode; and an assertion that the default mode is still name-only so an unlabeled historical comparison cannot silently mix regimes.

## B-01KYT9AT0CFBCVG13K6M0R0XCT the bench per-call total is not byte-reproducible across runs, so the reproducibility assertion I shipped in v0.9.0 flakes CI
kind: bug
state: done
created: 2026-07-30
targets: internal/bench/bench_test.go

MY BUG, shipped in v0.9.0 and caught by CI on the next PR: TestSchemaMeteringIsRealAndInert fails with per-call total is not reproducible: 4162B then 4168B. It passed locally on repeated runs and fails on the CI runner.

CAUSE. The assertion claims two Run calls over the same script and fixture must meter byte-identical per-call totals. That is not an invariant. Each Run mints fresh record IDs, IDs are time-ordered, and the adaptive short-prefix shortener picks a prefix length from what is unambiguous in that workspace - so two runs can render IDs of different widths and the totals differ by a few bytes. Locally the runs happened to land on the same widths; the CI runner is loaded enough that they did not.

WHY THE ASSERTION SHOULD GO RATHER THAN BE LOOSENED. Its stated purpose was to prove the schema metering does not perturb the fixture it measures. A verifier already established by mutation that it does NOT do that: two Run calls each build their own fresh fixture, so a perturbation happens identically in both and the totals still agree. The comment above it was corrected to say so, and to say the real protection is the signature - schemaBytes takes no workspace root and therefore has nothing to perturb. So the assertion pins nothing it claims to pin, and now it is actively wrong about a property the program does not have. Delete it; keep the schema-magnitude assertions in the same test, which ARE mutation-verified.

WORTH RECORDING: this is the same shape as the defects this session kept finding, inverted. Those were assertions that looked like coverage and caught nothing. This is an assertion that caught something - a real difference between two runs - and was WRONG to, because the difference is legitimate. Both come from writing an assertion without measuring whether the property holds.

FIX. Remove the reproducibility check and its now-false rationale. If a stability property is wanted later, it has to be stated against something that is actually stable - the metered schema and manifest sizes are, since they do not contain record IDs.

VERIFY. Run the bench test repeatedly under load and confirm it is green; assert the schema and manifest figures ARE stable across runs, since those carry no IDs.

## B-01KYTBR3BAF9KAQDD1N06FWX4R the doc comment above TestSchemaMeteringIsRealAndInert still describes the assertion that was removed from it
kind: bug
state: draft
created: 2026-07-30
targets: internal/bench/bench_test.go

Flagged by the verifier of B-01KYT9AT0CFBC as a non-blocking nit, filed rather than folded in because that record was already verified and archiving.

The doc comment above TestSchemaMeteringIsRealAndInert still ends with a sentence saying the reproducibility assertion below is kept because it is cheap and pins a different property, that the scripted total does not wander between runs. That assertion was REMOVED by B-01KYT9AT0CFBC, precisely because the scripted total DOES wander - each Run mints fresh record IDs and the adaptive shortener picks a prefix width from what is unambiguous in that workspace. The comment now describes code that is not there and asserts a property the program does not have.

WHY IT IS WORTH A RECORD rather than a silent edit: this is the fourth comment in one week found asserting something untrue, after the truncation-marker ordering claim, the schemas-exceed-64KB claim, and the two parser doc comments that said Escalate writes outcome=. Each was harmless alone and each cost a later reader real time. The pattern is that comments are edited when the code they sit above changes, and NOT when code they merely refer to is deleted.

FIX. Replace the trailing sentence with what the test now does: assert the SESSION measurements - schema and manifest - are stable across runs, because unlike the per-call total they carry no record IDs. Keep the explanation of why the per-call total is not stable, since that is the part a future reader needs in order not to re-add the assertion.

VERIFY. Read the comment against the test body and confirm every sentence describes code that exists. Cheap, and the only check that would have caught any of the four.
