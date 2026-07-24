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

## P-0067 ID reuse after compact (all kinds) + get does not render the ADR fields
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

Two defects found while migrating the design docs to ADRs, one of them serious. (1) ID REUSE AFTER COMPACT — lifecycle.maxNum (lifecycle.go ~632) determines the next item number by scanning journal CREATE events. compact deliberately folds create/move/rule/drift events away, keeping only reject/archive/compact. So once a context's journal has been compacted, every id whose create event was folded becomes invisible to the minter and gets handed out again. Observed live: after this repo's compact, minting four new ADRs produced ADR-0001..ADR-0004 — the numbers of four already-archived historical records, silently shadowing their tombstones (get ADR-0001 would resolve to the new item, the old record unreachable by id). This is not ADR-specific: proposals, tasks, bugs and research items are all minted through the same path, so any compacted repo can reissue P-/T-/B-/R- ids. Fix: maxNum must consider every journal event that carries an id for the kind (archive and reject events survive compaction and carry both id and k), plus live work.md items, not just creates — take the maximum across all of them. (2) GET DOES NOT RENDER THE ADR FIELDS — internal/item stores context/decision/consequences/status correctly (verified in work.md), but getItem's rendering never prints them, so an agent that fetches an ADR sees only question/options/choice and cannot read the very structure the ADR feature exists for. Exit criterion (explicit): a lifecycle test proves that after folding a create event out of a journal, minting the next id of that kind does NOT reuse the archived item's number; a server test proves get on an ADR renders context/decision/consequences/status; the four existing ADRs (0008-0011) still resolve; go test ./... -race green; check ok; lint 15/61/0. Rollback: revert. Scope: internal/lifecycle/lifecycle.go + test for the minter; internal/mcpserver/tools.go (getItem only) + tools_test.go for the rendering — two disjoint task scopes.

## T-0096 minter must not reuse ids whose create event was compacted away
kind: task
state: active
created: 2026-07-24
parent: P-0067

IMPLEMENTER IN A DEDICATED WORKTREE (pre-created at .claude/worktrees/impl-n, based on HEAD); NO lifecycle moves (orchestrator owns them); only lease claim, code+tests, lease release, report. Scope (disjoint, EXACTLY two files): internal/lifecycle/lifecycle.go, internal/lifecycle/lifecycle_test.go. A sibling implementer owns internal/mcpserver/tools.go and tools_test.go — do NOT touch them. THE BUG: maxNum(ws, kind) (~line 632) scans journal CREATE events to find the highest used number for a kind. compact folds create/move/rule/drift events away (it keeps reject/archive/compact), so after a compact the ids of folded-away items are invisible and get minted a second time. Proven live in this repo TWICE: after a compact, four new ADRs were minted as ADR-0001..0004 (numbers of four already-archived records), and a new proposal was minted as P-0067, the number of an already-archived proposal. This affects every kind (P/T/B/R/ADR) because they all mint through this path. THE FIX: maxNum must take the maximum over EVERY source that can still witness a used id — (a) journal events of ANY event type that carry both an id and the kind field k (archive and reject events survive compaction and carry them; keep create too), and (b) the live items currently in work.md across the workspace's context dirs (item.LoadAll or whatever this package already uses). Read the existing implementation first and keep its structure and idioms; the change is widening the scan, not rewriting the minter. Preserve the existing behavior that coord-backed minting (the Minter passed into Draft) still wins when present — you are fixing the local floor it is given. TESTS in lifecycle_test.go: (1) the regression: build a workspace where an item of some kind was created and archived, then rewrite its journal so ONLY the archive event remains (simulating a compact that folded the create away) and assert the next Draft of that kind mints a HIGHER number, never the archived one; (2) same for a rejected item (reject events also survive); (3) a live work.md item still bounds the floor; (4) an empty workspace still starts at 0001. Verify: go vet ./internal/lifecycle/ && go test ./internal/lifecycle/ -race && go test ./... -race && make build && ./bin/spectackle lint . (must stay 15/61/0). Report the diff and the regression test output. Rollback: git checkout the two files.

## T-0097 get must render the ADR fields (context/decision/consequences/status)
kind: task
state: active
created: 2026-07-24
parent: P-0067

IMPLEMENTER IN A DEDICATED WORKTREE (pre-created at .claude/worktrees/impl-o, based on HEAD); NO lifecycle moves; only lease claim, code+tests, lease release, report. Scope (disjoint, EXACTLY two files): internal/mcpserver/tools.go (the getItem rendering path ONLY — do not touch check, compact, find, draft or the move handler), internal/mcpserver/tools_test.go. A sibling implementer owns internal/lifecycle — do NOT touch it. THE GAP: internal/item stores the four ADR fields correctly (verified: a real ADR block in .spectackle/work.md contains context:, decision:, consequences: and status: lines) and the Item struct carries Context/Decision/Consequences/Status, but getItem's output never prints them — so `get ADR-0010` shows only the title, the kind/state/created header and the radio options/choice, and an agent cannot read the structured record the ADR feature exists to provide. THE FIX: in getItem, after the existing header/body rendering, emit the four fields when non-empty, in the classic ADR order context, decision, consequences, status, using the same dense one-field-per-line style the item header already uses (look at how rounds/grilled/needs are rendered and match it exactly — do not invent a new record letter or a JSON shape). Empty fields stay omitted (output diet, R-0001). Non-adr items must render byte-identically to today (their fields are empty) — assert that. TESTS in tools_test.go: (1) drive the decide tool over the wire to create an ADR with a context, answer it with a choice and consequences, then assert `get <ADR-id>` output contains the context text, the decision text, the consequences text and `status: accepted`; (2) assert a plain proposal's get output is unchanged (no stray empty field lines). Verify: go vet ./internal/mcpserver/ && go test ./internal/mcpserver/ -race && go test ./... -race && make build && ./bin/spectackle lint . (must stay 15/61/0). Report the exact get output for an ADR. Rollback: git checkout the two files.
