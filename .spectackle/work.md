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

## P-0070 purge stale pre-rename D-000x references from prose and comments
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: docs/roadmap.md, docs/design-wasm-parsers.md, internal/mcpserver/decide.go

The D->ADR rename (P-0056, T-0084) and the hand migration under P-0066 rewrote the records, but three prose sites still name identifiers that no longer resolve to anything: docs/roadmap.md M6 cell says "wazero deferred per D-0002"; docs/design-wasm-parsers.md heads a section "Re-measurement (D-0004, reopen-poc)" and repeats "reopening D-0004"; internal/mcpserver/decide.go carries a comment naming "this repo's D-0002" as its worked example. A reader following any of them gets nothing back from get or find.

Two of the three also carry stale SUBSTANCE, not just a stale id: the M6 roadmap cell still describes wazero as deferred, which ADR-0011 (stay on cgo past M6) and then ADR-0010 (pursue wasi-sdk grammars for CUDA/ObjC first, correctness-first) have superseded. Correcting the id without correcting the verdict would leave the roadmap lying with a valid link.

Scope is prose only. Test fixtures that use D-0001/D-0002 as arbitrary well-formed identifier strings (internal/journal, internal/item, internal/replay, internal/lifecycle tests) are NOT in scope: item.IDRe accepts any uppercase prefix by design, and those tests assert the grammar, not this repo's records.

## T-0100 retire the three stale D-000x prose references and refresh the M6 roadmap verdict
kind: task
state: active
created: 2026-07-24
parent: P-0070
targets: docs/roadmap.md, docs/design-wasm-parsers.md, internal/mcpserver/decide.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body before touching anything; do not explore beyond the files named here.

GOAL
The D->ADR rename and the authorized one-time hand migration rewrote the records but left three prose sites naming identifiers that no longer resolve. Fix the ids AND, where the surrounding sentence is also stale, the substance.

SCOPE (disjoint, lease exactly these three)
  docs/roadmap.md
  docs/design-wasm-parsers.md
  internal/mcpserver/decide.go   (comment text only, no code change)
Do NOT touch internal/resolve — a sibling task owns that whole directory right now. Do NOT touch test fixtures in internal/journal, internal/item, internal/replay or internal/lifecycle: they use D-0001/D-0002 as arbitrary well-formed identifier strings to assert item.IDRe's grammar, which accepts any uppercase prefix by design. Rewriting them would weaken a grammar test to chase a cosmetic match. .spectackle files are server-owned: never edit them by hand.

GROUND TRUTH (read these two records first, via the MCP get tool, not by grepping files)
  get id=ADR-0010  -> question: cgo vs wazero for C/C++ vs wasi-sdk grammars for CUDA/ObjC first. choice: secure or hand-compile wasi-sdk grammar wasm for CUDA/ObjC as the first buildable slice, then adopt wazero/tree-sitter for fidelity. status accepted. It explicitly supersedes the earlier defer verdict.
  get id=ADR-0011  -> question: adopt malivvan/tree-sitter now, invest in a wasi-sdk pipeline, or stay on cgo past M6. choice: stay on cgo past M6. status accepted, and its own consequences field records that it is SUPERSEDED by the correctness-first re-reading in ADR-0010.
The two together are the current position: cgo stays for now, wazero is not abandoned and not merely deferred — the blocking work is named and scoped (wasi-sdk-era grammar wasm for CUDA and ObjC does not exist yet).

EDITS
1. docs/roadmap.md, the M6 row's parenthetical. It currently ends "wazero deferred per D-0002". Replace that clause so it states the real position: cgo stays past M6 per ADR-0011, and wazero adoption is gated on wasi-sdk-era CUDA/ObjC grammar wasm existing per ADR-0010. Keep the cell's existing terse style and the rest of its content (cookbook live, Fortran/ObjC/Metal parsers shipped, GLSL parsed, 30 languages, Vulkan host-binding resolver). Note that the Vulkan clause is being changed by a sibling task in a separate worktree — leave the Vulkan wording exactly as it is so the two patches merge cleanly.
2. docs/design-wasm-parsers.md line ~141, the heading "## Re-measurement (D-0004, reopen-poc)", and line ~191 "reopening D-0004". D-0004 was the reopen-the-PoC decision; in the migrated records that line of decision is carried by ADR-0011 (the PoC scoring) feeding ADR-0010 (the correctness-first re-read). Point both mentions at the ADR that actually holds the content the sentence is making a claim about; if one sentence spans both, name both. Do not restructure the document.
3. internal/mcpserver/decide.go around line 229, the comment naming "this repo's D-0002" as a worked example of an item whose option set changed without migration. Update the identifier to whichever migrated ADR is that example, or, if no migrated ADR matches the described shape, rewrite the sentence to describe the shape without naming a record at all. Verify by actually reading the four ADRs (get id=ADR-0008 .. ADR-0011) — do not guess a number. This is a comment-only edit: the surrounding code must be byte-identical.

VERIFY
  go build ./...
  go test ./... 
  ./bin/spectackle lint
  grep -rn "D-00[0-9][0-9]" docs/ internal/mcpserver/   -> must return nothing
  grep -rn "ADR-00" docs/roadmap.md docs/design-wasm-parsers.md internal/mcpserver/decide.go   -> every id printed must exist; confirm each with get id=<ADR-id>

EXIT CRITERION
The two greps above behave as stated, every ADR id named in prose resolves through get, ./... green, lint clean. Then move the task to done.

ROLLBACK
Prose and one comment only; git checkout of the three files restores the prior state. No code path, schema, record or anchor is touched — the anchors.tsv rows for internal/mcpserver are keyed on code spans, and a comment-only edit inside a function body may still shift a span hash, so if check reports drift on an internal/mcpserver rule after this change, report it rather than healing it yourself.

HANDOFF
lease claim the three paths under this task id, work op=start, implement, run the verify block, work op=submit, then lease release. Report the exact grep output.
