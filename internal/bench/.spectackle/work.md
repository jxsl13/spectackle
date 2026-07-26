---
schema: v1
---

## T-01KYE2DGSDEG5R0P6C8WHP8WZB judge scores aggregate across runs: one summary line with validity ratio and calls and bytes spread, so variance is read from n runs, not one
kind: task
state: done
created: 2026-07-26

The judge history shows why one run cannot carry a verdict: at the same build, valid runs spanned 17 to 46 calls and 2279 to 5896 bytes (the wanderer), and the baseline pair spread 831 bytes at identical call counts. Change: bench -agent-score accepts a comma-separated list of judged workspace dirs; a single dir keeps todays exact output; for n greater than one it prints the per-run agent lines prefixed with the dir basename, then one aggregate line — agents n=<n> valid=<v>/<n> calls=<min>/<med>/<max> bytes=<min>/<med>/<max> — and the process exits non-zero when ANY run missed its goals, because a text change that makes one judge in three fail is a regression however good the median looks. Median over mean: a single wanderer must not drag the central figure. Unit tests: aggregation math over three synthetic scores including one invalid (exit semantics), single-dir output byte-identical to the previous format. VERIFY: go test ./internal/bench/ -count=1 green; a live three-judge batch scored with one command recorded in the archive note.
