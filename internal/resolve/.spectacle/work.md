---
schema: v0
---

## P-0008 CUDA chain live: CudaParser + launch edges - saxpy triple-hop reproduces
kind: proposal
state: active
created: 2026-07-24
targets: internal/resolve/cuda.go, go:saxpy.Saxpy

New index.CudaParser (line scanner, .cu/.cuh): __global__ defs -> cu:<name> kernel nodes, extern-C host wrappers -> c:<name> fn nodes (upgrading cgo stubs to real located nodes). Implement resolve.CudaResolver: kernel<<<...>>> launch sites -> ELaunch edge from enclosing extern-C wrapper to cu:<kernel>. Exit: on examples/saxpy the full chain go:saxpy.Saxpy -cgo-> c:launch_saxpy -launch-> cu:saxpy_kernel reproduces via get depth=2.

## T-0013 cuda: CudaParser (line scanner) + launch-edge resolver, saxpy triple-hop
kind: task
state: done
created: 2026-07-24
parent: P-0008

Scope files ONLY: new internal/index/cudaparser.go + cudaparser_test.go; rewrite internal/resolve/cuda.go + new cuda_test.go. Do NOT touch internal/mcpserver (orchestrator registers the parser). CudaParser: Lang=LangCuda, Ext .cu/.cuh, regex line scan: __global__ func def -> cu:<name> KKernel node; extern "C" function DEFINITION -> c:<name> KFunc node; Line set, EndLine=closing-brace line if trivially found else Line. Resolver: fs.ByLang(LangCuda), track current enclosing extern-C wrapper while scanning lines; kernel<<< launch -> ELaunch edge c:<wrapper> -> cu:<kernel> at launch line; skip launches outside a known wrapper. Acceptance: indexer e2e test on examples/saxpy asserts chain go:saxpy.Saxpy -cgo-> c:launch_saxpy -launch-> cu:saxpy_kernel and c:launch_saxpy now has real File:Line. make all green.
