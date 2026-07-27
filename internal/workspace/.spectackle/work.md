---
schema: v1
---

## T-01KYJN4AP3E1M8MHQPDE17E89Z internal/benchmark: record schema, canonical frame keys, keyed store with depth-trimmed history
kind: task
state: draft
created: 2026-07-27
parent: P-01KYJMVX2QES89YTP3KXSJPA7J
grilled: 2026-07-27 open=2
targets: internal/benchmark, internal/workspace

Foundation task of P-01KYJMVX2Q; the full design is the wf_0ed39152 synthesis (scratchpad bench-synthesis.md holds the authoritative copy - read sections 1-6 and 10 before writing code). BUILD internal/benchmark: record.go - Record{ID, Name, Key, Ver, Frame map[string]string, Metrics []Metric{Name,Unit,Dir,Noise}, Impls []Impl{Label,Src,Res map[string]float64}, T, Ag, Tool, Note, Dir}; key.go - CanonicalKey(name, frame): fold name and dim keys/values (lower, trim), sort dims k=v joined with | after validating REQUIRED os/arch/cpu/ram/gpu present (sentinels none and any are legal values per ADR-01KYJMWE1N), refuse forbidden separator characters in names/dims (=|;: and whitespace inside tokens), deterministic across insertion order; store.go - Load(path) parsing ndjson tolerantly (unknown fields preserved on rewrite via ordered generic decode like migrate does, or document lossy-struct round-trip as a decision), key recomputed and VERIFIED against the stored field at load (mismatch = corruption line, quarantine the line not the file), union-merge dedup (same ID wins once; same key+ver conflict resolves deterministic by T then ID), Put(rec) assigning Ver = head+1 on content change / idempotent unchanged detection byte-stable, TrimHistory(key, depth) keeping the newest depth versions; workspace wiring - BenchPath(ctx) beside SpecPath, bench.ndjson in the context-discovery allowlist and the gitattributes ensureLines (merge=union), config knob benchmarks: history: (default 1, load fallback) parallel to CompactCfg. NO tool surface in this task (task 2). TESTS: canonicalization matrix (order-independence, case folding, sentinel legality, forbidden chars refused, missing required dim refused), collision cases (two hosts differing only in ram = different keys; any collapses them), Put version monotonicity + idempotent replay + content-change detection, trim at depth 1 and depth 3, load-verify quarantine, union-merge dedup determinism. VERIFY: go build ./... && go test ./internal/benchmark/ ./internal/workspace/ -count=1 && gofmt -l . empty. SCOPE: the new package + workspace wiring only. ROLLBACK: revert.
