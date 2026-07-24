---
schema: v0
---

## ADR-0008 Should GoParser.callEdges keep minting call edges syntactically, or add a go/types semantic pass to fix silently-dropped chained-selector calls?
kind: adr
state: done
created: 2026-07-24
context: callEdges mints ECall edges from source-text spelling; any chained selector (s.cd.Sweep()) has fn.X typed as another SelectorExpr, falls through the switch, and the call is silently dropped instead of emitted as a prunable dangling edge. Confirmed live: get go:coord.DB.Sweep depth=1 shows zero incoming edges although internal/mcpserver/swarm.go:51 calls it. packages.Load type-checks the whole module transitively, ~10-100x slower than a bare go/parser.ParseFile per file — unacceptable as the steady-state per-file cost in the walk-hash-cache pipeline. Source: docs/design-go-types-calls.md.
decision: two-tier hybrid: syntactic pass stays the fast always-on path, add a go/types upgrade pass scoped to IndexAll, cached by module hash
consequences: Direct and embedded-selector method calls (the s.cd.Sweep() class of gap) become resolvable; regression test: get go:coord.DB.Sweep depth=1 must show go:mcpserver.Server.preCall as an incoming caller. Interface dispatch is explicitly deferred to M3+, resolving only to the interface method node. The cgo boundary stays owned by resolve.CgoResolver. Cross-module resolution beyond the repo root is out of scope for the first cut.
status: accepted

kind: radio
option: keep syntactic-only resolution (status quo)
option: replace the syntactic pass entirely with go/packages+go/types on every parse
option: two-tier hybrid: syntactic pass stays the fast always-on path, add a go/types upgrade pass scoped to IndexAll, cached by module hash
choice: two-tier hybrid: syntactic pass stays the fast always-on path, add a go/types upgrade pass scoped to IndexAll, cached by module hash

## ADR-0009 Should spectackle build node-removal machinery for incremental IndexPaths, or keep the full-IndexAll rebuild?
kind: adr
state: done
created: 2026-07-24
context: Measured on this repo (73 files, 13.7k LOC): IndexAll is 40-42ms cold / 14-15ms warm in memory, 340-390ms cold / 19-20ms warm on sqlite. A 5000-file synthetic tree (245k LOC, 2.4x the M4 gate) warm-rebuilds in 333-383ms (mem) / 591-748ms (sqlite) — 7-15x under the M4 budget of 100k LOC indexed under 5s warm. Extrapolating the sqlite-warm slope (~120-150us/file), full rebuild only risks the 5s budget around 30k-40k files (~1.5-2M LOC), 15-20x past the gate's reference size. Removal machinery's payoff is bounded by savings against an already sub-second warm baseline. Source: docs/design-incremental-index.md.
decision: rebuild-from-cache (status quo): IndexPaths stays a no-op, full IndexAll every refresh
consequences: Zero new code and no new invariants beyond SPX-GRA-001/002; removal complexity is deferred until warm IndexAll approaches multi-second cost. Stated reopen criterion: once a real target repo's warm IndexAll measures within 2x of the 5s M4 budget, reopen starting from the per-file ownership index (smaller and more testable than the sweep scheduler). Two unrelated gaps flagged as worth fixing regardless: batching store.Put writes, and scoping ResolveTypedCalls' module-hash key to the changed file set.
status: accepted

kind: radio
option: per-file ownership index plus RemoveFile(path), then reparse
option: generation-stamped nodes with a lazy sweep
option: rebuild-from-cache (status quo): IndexPaths stays a no-op, full IndexAll every refresh
choice: rebuild-from-cache (status quo): IndexPaths stays a no-op, full IndexAll every refresh

## ADR-0010 Given correctness-first evaluation, should spectackle stay on cgo tree-sitter, adopt wazero/wasm for C/C++ only, or secure WASI grammars for CUDA/ObjC first?
kind: adr
state: done
created: 2026-07-24
context: Evaluation axis (user steer 2026-07-24): correctness first, performance second — a real tree-sitter grammar gives fidelity on hard C/C++ constructs (macros, multi-line declarators, templates) that langspec's regex approximation cannot match. Measured: bit-level parity with the cSpec oracle (1151 symbols, 0 regressions); latency 2.0-5.4s per 100k LOC, reframed as NOT decisive because it is a one-time initial-read cost the M2 parse-blob cache amortizes; binary size 9.24MB, within the 10MB budget. Availability: no WASI-sdk-era grammar wasm exists for CUDA (zero release assets) or ObjC (only a pre-wasi-sdk Emscripten build). Source: docs/design-wasm-parsers.md, final Recommendation section.
decision: secure or hand-compile wasi-sdk grammar wasm for CUDA/ObjC as the first buildable slice, then adopt wazero/tree-sitter for fidelity
consequences: Enables the correctness upgrade over langspec's regex approximation once CUDA/ObjC grammars exist, but defers the actual backend migration until that grammar-availability work is done (upstream-adjacent build work, not a drop-in). The ~9.24MB binary cost for C+C++ is accepted as within budget. Latency is explicitly not gated on — it is absorbed by the parse-blob cache. Still open: which loading strategy wins (static-binary embedding vs a runtime-pluggable grammar loader, unbuilt anywhere today). langspec remains the shipping breadth track, unaffected either way. Supersedes the earlier defer verdict recorded in the same document.
status: accepted

kind: radio
option: stay on cgo tree-sitter, do not pursue wazero/wasm further
option: adopt wazero/wasm now for C/C++ only, leaving CUDA/ObjC on cgo permanently
option: secure or hand-compile wasi-sdk grammar wasm for CUDA/ObjC as the first buildable slice, then adopt wazero/tree-sitter for fidelity
choice: secure or hand-compile wasi-sdk grammar wasm for CUDA/ObjC as the first buildable slice, then adopt wazero/tree-sitter for fidelity

## ADR-0011 Scored against the four PoC exit criteria, should spectackle adopt malivvan/tree-sitter now, invest in a wasi-sdk pipeline, or stay on cgo past M6?
kind: adr
state: done
created: 2026-07-24
context: PoC scored on a 51-file/9621-LOC corpus against design-wasm-parsers section 5's four criteria: PARITY PASS (1151/1151 matched, 0 regressions); SIZE PASS (8.82MB delta, ~88% of the 10MB budget, vs 22.1MB for the production binary with all four cgo backends); LATENCY FAIL (2.06-6.4s per 100k LOC vs 52-73ms for cSpec, only 0.8-2.4x headroom, violating the 5s budget in 2 of 5 runs; root cause isolated to the binding's per-call uint64 allocation churn, heap 46MB to 476MB over 6 GC-disabled passes, not wazero itself); AVAILABILITY FAIL (tree-sitter-cuda has zero published wasm assets; tree-sitter-objc's only wasm predates the wasi-sdk switch). Source: docs/design-wasm-poc-c.md.
decision: stay on cgo tree-sitter past M6; do not adopt this PoC's stack
consequences: SUPERSEDED by the correctness-first re-reading of this same PoC data (see the wazero grammar-availability ADR): that later decision pursues wasi-sdk grammars rather than staying on cgo indefinitely. As written, this verdict kept langspec and the existing cgo backends as the investment target. The size result is retained as evidence that a single-language wasm engine fits budget. Criteria latency and availability can be re-scored cheaply by rerunning the same PoC once a batched-read binding or wasi-sdk-era CUDA/ObjC wasm appears. poc/wasmparse remains disposable by design.
status: accepted

kind: radio
option: adopt malivvan/tree-sitter (wazero/wasm) as-is for the C parser
option: invest in a wasi-sdk build pipeline and a batched-read binding now
option: stay on cgo tree-sitter past M6; do not adopt this PoC's stack
choice: stay on cgo tree-sitter past M6; do not adopt this PoC's stack

## P-0083 the dev server always runs the current build: one command to rebuild and restart, and a hint when it drifts
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: Makefile, CONTRIBUTING.md

This repository develops itself with itself, so the resident server an agent drives IS the product under change. Every merged feature or fix makes the running binary older than the code that describes it, and the gap is invisible from the inside: tool output looks plausible because it comes from a real server, just not from the one in the tree.

Measured instances from this development cycle, both of which cost real time. A compact hint appeared broken and was investigated as a defect; the binary was 41 minutes older than its sources and the feature had in fact shipped. Separately, a resident server serving from a graph built at startup produced two false drift verdicts that auto-healed anchors with hashes for spans that were not the node — the same staleness family, one level up, and the reason DRF-003 now exists.

Two halves, because either alone leaves the hole open. Making the restart cheap is not enough if nobody notices they skipped it; detecting drift is not enough if fixing it is a five-command ritual nobody remembers.

Half one, a single command that rebuilds and restarts. The pieces already exist and are not yet composed: the Makefile builds, serve -http runs resident, and -pidfile (added this cycle) makes stopping a kill against a known file instead of a pgrep. Composing them must be idempotent and must never leave two servers bound to one port, since a half-dead second server is worse than a stale first one. Readiness has to be proven by an actual tool call rather than by a listening socket — the process binds before it finishes indexing, so a socket check would hand back a server that answers nothing.

Half two, the server notices for itself. It can compare its own executable's timestamp against the newest source file under its root and say so, exactly as the compact hint already nudges at a journal threshold — same debounced, once-per-crossing shape, so it informs without nagging. This turns an operator discipline into a property of the system, which is the whole argument for it: a rule nobody can forget beats a rule everybody agrees with.

Rejected: rebuilding automatically inside the server. A process that replaces its own binary mid-session would invalidate every in-flight lease and worktree, and a build failure would leave the agent with no server at all. Reporting is safe; self-surgery is not.

Rejected: a file watcher. It adds a dependency and a background goroutine to answer a question that a stat at tool-call time already answers, and the answer is only interesting when someone is actually using the server.
