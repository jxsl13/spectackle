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

## T-0097 get must render the ADR fields (context/decision/consequences/status)
kind: task
state: active
created: 2026-07-24
parent: P-0067

IMPLEMENTER IN A DEDICATED WORKTREE (pre-created at .claude/worktrees/impl-o, based on HEAD); NO lifecycle moves; only lease claim, code+tests, lease release, report. Scope (disjoint, EXACTLY two files): internal/mcpserver/tools.go (the getItem rendering path ONLY — do not touch check, compact, find, draft or the move handler), internal/mcpserver/tools_test.go. A sibling implementer owns internal/lifecycle — do NOT touch it. THE GAP: internal/item stores the four ADR fields correctly (verified: a real ADR block in .spectackle/work.md contains context:, decision:, consequences: and status: lines) and the Item struct carries Context/Decision/Consequences/Status, but getItem's output never prints them — so `get ADR-0010` shows only the title, the kind/state/created header and the radio options/choice, and an agent cannot read the structured record the ADR feature exists to provide. THE FIX: in getItem, after the existing header/body rendering, emit the four fields when non-empty, in the classic ADR order context, decision, consequences, status, using the same dense one-field-per-line style the item header already uses (look at how rounds/grilled/needs are rendered and match it exactly — do not invent a new record letter or a JSON shape). Empty fields stay omitted (output diet, R-0001). Non-adr items must render byte-identically to today (their fields are empty) — assert that. TESTS in tools_test.go: (1) drive the decide tool over the wire to create an ADR with a context, answer it with a choice and consequences, then assert `get <ADR-id>` output contains the context text, the decision text, the consequences text and `status: accepted`; (2) assert a plain proposal's get output is unchanged (no stray empty field lines). Verify: go vet ./internal/mcpserver/ && go test ./internal/mcpserver/ -race && go test ./... -race && make build && ./bin/spectackle lint . (must stay 15/61/0). Report the exact get output for an ADR. Rollback: git checkout the two files.

## P-0068 instructions: brownfield import recipe + compacted record-keeping rule
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

Two additions to the self-bootstrapping instructions manifest. (A) BROWNFIELD IMPORT — the manifest teaches the change loop but assumes a repo that already has contracts; it says nothing about onboarding an existing codebase, which is the normal first contact. Worked-out approach, six steps, each mapped to existing tools so nothing new is built: (1) INDEX FIRST — the graph costs nothing to obtain and needs no decisions; state/reindex yields nodes and edges immediately, giving real node ids to anchor against later. (2) SURVEY IN PARALLEL — fan out READ-ONLY subagents over disjoint subtrees (one per top-level package or module), each reporting the subtree's purpose, the invariants its code, tests and docs already assert, and candidate contracts. Read-only means no leases and no work.md contention, so the fan-out is unconstrained. (3) ORCHESTRATOR MINTS — the orchestrator turns the survey into rule op=add contracts scoped per context dir, anchored via applies to real node ids from step 1. Implementers never hand-write spec files; the server composes and lints. (4) CAPTURE EXISTING DECISIONS — design or ADR-style documents become adr items via decide (context, decision, consequences, status); pure reference documentation stays documentation. (5) BASELINE THE DRIFT — run check until clean; the stamped anchors are the baseline from which drift detection becomes meaningful. (6) THEN THE NORMAL LOOP — find scope=rejection, draft, grill, implement. Guardrails to state explicitly: encode only invariants the code, tests or docs actually assert, never invented ones; start with the load-bearing few; let check's coverage gaps reveal what is still unowned rather than trying to cover everything at once. (B) COMPACTED RECORDS — item bodies must record user input as compacted substance (constraints, decisions, measurements, rejected alternatives), never as verbatim quotes, which bloat records without adding information. This is a token-economy rule of the same family as the existing output diet. Exit criterion: the instructions manifest carries both paragraphs, concise enough not to bloat the handshake; a test asserts both are present; docs/agent-workflow.md documents the brownfield recipe in prose; go test ./... -race green; lint 15/61/0. Rollback: revert. Scope: internal/mcpserver/server.go, internal/mcpserver/server_test.go or the existing instructions test, docs/agent-workflow.md.

## T-0098 add BROWNFIELD and RECORDS paragraphs to the instructions manifest
kind: task
state: active
created: 2026-07-24
parent: P-0068

IMPLEMENTER IN A DEDICATED WORKTREE (pre-created at .claude/worktrees/impl-p, based on HEAD); NO lifecycle moves; only lease claim, edit+tests, lease release, report. Scope (disjoint, EXACTLY three files): internal/mcpserver/server.go, internal/mcpserver/tools_test.go (the instructions test lives there — see TestInstructionsTeachTokenEconomy), docs/agent-workflow.md. Siblings own internal/lifecycle and the getItem path of internal/mcpserver/tools.go — you touch tools_test.go only, never tools.go. EDIT 1 — server.go: the instructions const is one Go raw string whose paragraphs are newline-separated (lifecycle loop, SWARM:, ORCHESTRATION:, TOKEN ECONOMY:). Append two more paragraphs, in this order, verbatim: >>>BROWNFIELD IMPORT: onboarding an existing repo, in order. (1) Index first: state/reindex yields the graph immediately and costs no decisions — you get the real node IDs everything anchors to. (2) Survey in parallel: fan out READ-ONLY subagents over disjoint subtrees (one per top-level package/module); each reports the subtree's purpose, the invariants its code/tests/docs already assert, and candidate contracts. Read-only means no leases and no work.md contention, so fan out as wide as the tree. (3) Mint centrally: the orchestrator turns that survey into rule op=add contracts, scoped per context dir and anchored via applies to node IDs from step 1 — implementers never hand-write spec files, the server composes and lints. (4) Capture decisions: existing design/ADR documents become adr items via decide (context, decision, consequences, status); pure reference docs stay docs. (5) Baseline: run check until clean — the stamped anchors are the point from which drift detection means anything. (6) Then the normal loop: find scope=rejection, draft, grill, implement. Encode only invariants the code/tests/docs actually assert, never invented ones; start with the load-bearing few and let check's coverage gaps show what is still unowned.<<< and >>>RECORDS: write item bodies as compacted substance — constraints, decisions, measurements, rejected alternatives and why. Never paste verbatim user quotes or transcript excerpts: they bloat every later read of the record without adding information. Same token-economy family as the output diet.<<< Both texts contain no backticks, so they paste safely into the raw string. EDIT 2 — tools_test.go: extend the existing instructions test (or add a sibling test next to it, matching its style) asserting the manifest contains BROWNFIELD IMPORT, the phrase 'Survey in parallel', RECORDS, and 'Never paste verbatim'. EDIT 3 — docs/agent-workflow.md: add a section titled 'Importing a brownfield repo' presenting the same six steps in prose with the two guardrails, placed after the Fan-out section and before the worktree/lifecycle-state section. Keep it tight (roughly 25-35 lines) and consistent with the document's existing voice. Verify: go vet ./internal/mcpserver/ && go test ./internal/mcpserver/ -race && go test ./... -race && make build && ./bin/spectackle lint . (must stay 15/61/0). Report the two manifest paragraphs as they ended up and the test result. Rollback: git checkout the three files.
