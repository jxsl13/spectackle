---
schema: v1
---

## T-01KYH54HE1EYZTETA37HFTNPBZ outcome brief tool list is complete: validate and work were discoverable only through refusals, costing every require judge a loop
kind: task
state: active
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/bench/outcome.go, internal/bench/agent.go

EVIDENCE (T-01KYGX9P and T-01KYH1GK judge reports, verbatim across two independent runs): the outcome briefs Available tool names line omits validate and work; both require-side judges discovered validate only when the archive refused, and one confirmed it against the public source before trusting it - a full discovery loop of tokens per judge that measures brief incompleteness, not agent skill. The basic/tricky briefs share the list shape - audit them for the same omissions while here (worktree scenario judges used work, so its brief presumably names it - verify).

WHAT TO CHANGE: the tool-name lines in outcomeBrief (and any sibling brief missing tools its scenario can legitimately need) gain validate and work; nothing else in the briefs changes - the scenario philosophy (no other documentation, outputs guide you) stands, this is the difference between a MAP that is complete and one that is wrong. NON-NEGOTIABLE: a test asserts every brief consts tool list is a subset-complete enumeration of the registered tool surface relevant to its scenario (at minimum: outcome lists validate; no brief lists a tool that does not exist - pin against a canonical slice, not prose). VERIFY: build/test/vet/gofmt; lint; check ok; paste the changed lines. SCOPE: the brief consts + the list test. ROLLBACK: revert. REPORT: the diff lines, which sibling briefs needed changes, the test.
