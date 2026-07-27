---
schema: v1
---

## B-01KYGZNT0WEZ4V5B7VTVWADFDR meter shim sequence check false-disqualifies judges that batch parallel tool calls
kind: bug
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/bench/agent.go

OBSERVED (T-01KYGX9P warn-2 run): agent verdict DISQUALIFIED - meter sequence gap: line 27 carries seq 32 - on a run whose meter.log holds 45 entries with ZERO duplicate seq numbers. Mechanism: the shim computes SEQ from wc -l then appends in two non-atomic steps; a judge issuing PARALLEL tool calls (harness agents batch independent calls routinely) interleaves appends so a higher seq lands on an earlier line. The scorer reads line-order-vs-seq monotonicity as log truncation/tampering - a false positive that costs an entire run from every batch containing one parallel-calling judge, and the all-valid rule then refuses the batchs efficiency figure (correctly, but for a harness reason). EXPECTED: metering robust under concurrent shim invocations. OPTIONS, pick with reasoning: (a) flock around the read-count-append critical section (shell flock is available on darwin/linux CI); (b) drop line-position semantics - scorer sorts by seq and checks for duplicates and holes only, tolerating out-of-order appends (weakens the truncation check only for the tail); (c) append first with a placeholder and renumber at score time from content hashes. Bias toward (a): smallest diff, keeps every existing integrity property. TESTS: a synthetic meter.log with out-of-order-but-complete seqs scores VALID; a log with a genuine hole still disqualifies; a concurrent-invocation stress (N parallel shim calls) yields a complete monotone-on-sort log. VERIFY: build/test/vet/gofmt; the warn-2 log rescored valid after the fix (kept as a fixture). SCOPE: the shim heredoc + scorer sequence check + tests. ROLLBACK: revert.
