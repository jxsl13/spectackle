---
schema: v0
---

## R-0005 per-language parser gap analysis: langspec chain vs tree-sitter fidelity for symbol extraction and graph search
kind: research
state: active
created: 2026-07-25
targets: internal/langspec

QUESTION
Does the shipped parser surface (go/asm/cuda hand-written + 29 langspec data languages) extract symbols and call edges faithfully enough for the graph-search use case (find scope=code, get depth impact), measured per language against what a tree-sitter grammar (the gated wazero path, ADR-0010/ADR-0011) would extract?

METHOD
One read-only analysis agent per language, in parallel. Each agent: reads its language's parser (langspec data file or hand-written parser), writes one adversarial-but-idiomatic sample (~60-120 lines) into a scratch workspace exercising the constructs tree-sitter handles trivially (multi-line declarations, nesting, generics/templates, macros, operator/receiver forms, language-specific call syntax), runs the real indexer over it (reindex + find scope=code against the scratch root), and diffs extracted nodes/edges against the ground truth it authored. Output: missed or mis-spanned constructs with severity for graph search, call-edge verdict, and whether tree-sitter fidelity would actually change the outcome. Empirical evidence only — no speculation from reading regexes alone.

CONTEXT (from the record)
The wazero path is availability-gated (no wasi-sdk CUDA/ObjC grammar wasm) and latency-red (GC churn in the malivvan binding); the langspec chain is the production path with parity PASS on C (1151/0/0). This analysis identifies which languages, if any, have gaps that would justify re-scoring that decision or hardening a langspec entry.

NOTED BEFORE FAN-OUT
python.go and javascript.go are the only langspec files without a _test.go sibling — a test-coverage gap regardless of fidelity findings.

EXIT CRITERION
A per-language verdict matrix (ok | minor-gaps | major-gaps, with concrete missed constructs and severities), an aggregate answer to the question, and lifecycle follow-ups drafted for any real gap worth fixing.
