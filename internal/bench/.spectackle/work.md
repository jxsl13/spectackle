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

## B-01KYTES575EBVSCCKA08P0BF32 the manifest is static text is imprecise: manifest() appends a runtime-resolved URL from build info
kind: bug
state: done
created: 2026-07-30
targets: internal/bench/bench_test.go

Found by the verifier of B-01KYTBR3BAF9K, which was itself a record about a comment asserting something untrue - so leaving this unfixed would be the same defect one layer down.

The comment above TestSchemaMeteringIsRealAndInert justifies asserting that ManifestBytes is stable across runs by saying the manifest is static text. It is not quite: manifest() appends a defect-report URL resolved at runtime from build info, so it is not a compile-time literal.

WHY IT IS NOT A FALSEHOOD, and why the record still stands: that value is invariant across the two Run calls under test, because both drive the SAME binary. So ManifestBytes cannot wander and the functional claim the comment makes is true. The imprecision is in the REASON given, not in the conclusion - which is exactly the kind of thing that misleads a later reader who changes manifest() and trusts the stated rationale.

FIX. Say what actually holds: the manifest carries no record IDs and is invariant for a given binary, which is the property the assertion depends on. Do not say static.

WHY THIS WAS NOT FOLDED INTO B-01KYTBR3BAF9K: that record already carried a passing verdict, and editing it afterwards would have made the archive gate refuse as stale - correctly, since the verdict binds the diff. Filing was the cheaper honest path, not an avoidance.

VERIFY. Read the sentence against manifest() and confirm every clause is true of the code as written. While in the file, re-run the sweep for other comments describing code that no longer exists - a sub-agent found none on the last pass, so this is a spot check rather than an expected find.
