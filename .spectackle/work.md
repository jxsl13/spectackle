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

## P-0090 docs/tools.md is out of sync with the shipped tool surface, which SPX-REPO-001 forbids
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: docs/tools.md

SPX-REPO-001 requires the repository to keep every MCP tool schema in docs/tools.md consistent with the Go structs in internal/mcpserver/tools.go. It currently does not, and the gap is measurable: the document has no mention of draft's refs argument, none of swarm's q free and q held records, none of the commands tool's new default-versus-explicit arguments, and none of the stale-binary hint record. Four features shipped in the last two rounds without their documentation.

This matters beyond tidiness because of what the document is for. Agents parse the dense record grammar it specifies; a record kind that exists in output but not in the grammar is a record an agent has no reason to expect and no definition to interpret. The tool descriptions carry the arguments, but the grammar lives only here.

Scope discipline: document what is SHIPPED, not what is planned. The compact tool's redundant-rejection reporting is being implemented in parallel right now and must not be documented until it exists — a document describing an unimplemented record is worse than a missing one, because a reader cannot tell which half is true.

Rejected: regenerating this document from the Go structs. The jsonschema tags carry argument descriptions but not the output grammar, which is the more valuable half and is not derivable from any struct. A generator would have to be told the grammar anyway, at which point it is a second source of truth rather than one.

## T-0124 docs/tools.md: document refs, the swarm queue records, the commands arguments and the stale hint
kind: task
state: active
created: 2026-07-24
parent: P-0090
targets: docs/tools.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This is a documentation task: no Go code changes at all.

GOAL
SPX-REPO-001 requires docs/tools.md to stay consistent with the Go structs in internal/mcpserver/tools.go. Four shipped features are missing from it. Close the gap.

SCOPE (lease exactly one file)
  docs/tools.md
Do NOT touch internal/ or cmd/ at all — a sibling task holds tools.go right now. Do NOT touch README.md or other docs. .spectackle files are server-owned: never edit them by hand.

WHAT IS MISSING — verify each against the CODE before writing, do not trust this list blindly
  1. draft's `refs` argument: item IDs this item cites, any kind to any kind, no lifecycle meaning (unlike parent, which is structural ownership, and needs, which means blocked-on). Unknown, malformed or self-referencing ids are refused at the write path with `! ARG E - unknown refs: <ids>` and nothing is persisted. get renders `refs <ids>` when non-empty. Read draftIn and getItem in tools.go.
  2. swarm's queue records: `q free <id> <title>` for approved work no held lease collides with, and `q held <id> <agent> <path>` naming the holder and the colliding path. Also the truncation line swarm emits on overflow. Read swarm.go.
  3. the commands tool's arguments: the default set is three commands (the lifecycle entry point, state, generate); `all` and `commands` request the six exploration commands, and the requested set UNIONS with the default rather than replacing it. Read commandsIn in commands.go.
  4. the stale-binary hint record emitted by postCall when the running executable is older than the sources it serves. Read swarm.go.

SCOPE DISCIPLINE — the one thing that must not go wrong
Document what is SHIPPED, not what is planned. A sibling task is adding redundant-rejection reporting to compact RIGHT NOW; those records do not exist yet and must NOT appear in this document. A document describing an unimplemented record is worse than a missing one, because a reader cannot tell which half is true. If you are unsure whether something is shipped, check the code in this worktree — if it is not there, it does not go in.

HOW TO WRITE IT
Match the file's existing structure exactly: it has a record-grammar table near the top and one numbered section per tool. Put each addition where its neighbors already live rather than appending a new section at the end. The grammar table is the part agents actually parse — a record kind that appears in output but not in that table is one an agent has no definition for.
Also check the tool count and any summary line at the top: the header states how many tools exist and in which families. If a count is now wrong, fix it.

VERIFY
  go build ./... && go test ./...   (must still pass — you changed no code, this is the guard that you did not)
  /home/user/spectackle/bin/spectackle lint
  For each of the four items, re-read the corresponding Go source and confirm every argument name, record shape and field you documented matches character for character.

EXIT CRITERION
All four documented and verified against source, nothing unimplemented documented, no file other than docs/tools.md modified, tests and lint unchanged and green.

ROLLBACK
Documentation only. git checkout docs/tools.md restores the prior state; no code, schema, record or anchor is touched.

REPORT BACK
What you found for each of the four when you checked it against the code, anything the code does that differs from this brief's description of it, whether the tool count needed fixing, and anything you deliberately did NOT do.
