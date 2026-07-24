---
schema: v0
---

## P-0066 ADR migration tool: server-side rewrite of legacy D-ids, and stale pre-rebrand rule text
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

Two related gaps found by inspecting the living spec after the ADR rename landed. (1) MIGRATION IS BLOCKED WITHOUT A TOOL: the user asked for a FULL migration of legacy decision records to ADR ids. Parts 1+2 shipped (kind/ID rename, ADR fields, searchable). Part 3 cannot be done the way the others were: the two legacy records ADR-0010/ADR-0011 are now archived, so they live only as journal tombstones, and rewriting journal history by hand is exactly what the architecture forbids (the LLM never edits .spectackle; only the server writes it). The honest fix is to give the SERVER a migration path: a `migrate-adr` CLI subcommand that rewrites D-<nnnn> ids to ADR-<nnnn> across every .spectackle work.md and journal.ndjson in the workspace, dry-run by default, idempotent, refusing to run when a target ADR id already exists. That turns a forbidden hand-edit into an auditable, testable server operation. (2) STALE PRE-REBRAND RULE TEXT: root spec.md still carries pre-rebrand wording — SPX-ARC-001 says 'The spectacle server' and SPX-ARC-005 says '.spectacle folders' (single c, the old name), and SPX-SWM-007 still says the server mints 'a linked `decision` item' although the kind is now adr. These are contract sentences an agent reads as ground truth, so the drift is user-visible and worth fixing via rule op=edit (server write path, orchestrator's job). Exit criterion (explicit): a migrate-adr subcommand exists, defaults to dry-run listing every id it would rewrite, applies only with an explicit flag, is idempotent (second run reports nothing), refuses on id collision, and is covered by tests over a fixture workspace containing both a live work.md item and journal tombstones; the three stale rule sentences read correctly (spectackle, .spectackle, adr) and lint stays clean. Rollback: revert the tool; rule edits are revertible via rule op=edit. Scope: internal/migrate (new) + cmd/spectackle/main.go + tests for the tool; the rule text fixes are orchestrator-side rule calls.

## T-0094 migrate-adr CLI: server-side rewrite of legacy D-ids to ADR-ids
kind: task
state: active
created: 2026-07-24
parent: P-0066

IMPLEMENTER IN A DEDICATED WORKTREE (pre-created at .claude/worktrees/impl-m, based on HEAD); NO lifecycle moves (orchestrator owns them); only lease claim, code+tests, lease release, report. Scope (disjoint): internal/migrate/ (NEW package: migrate.go + migrate_test.go), cmd/spectackle/main.go, cmd/spectackle/main_test.go. A sibling implementer owns README.md and docs/ RIGHT NOW — do NOT touch any doc file. Do NOT touch internal/mcpserver, internal/workspace, internal/lifecycle, internal/item. Goal: the ONLY sanctioned way to rewrite legacy decision ids is a server-side operation; hand-editing .spectackle is forbidden by the architecture. Build `spectackle migrate-adr [-root DIR] [-apply]`. Behavior: (1) Walk every .spectackle context bundle in the workspace (use the same discovery the rest of the code uses — workspace.Detect for the root, then workspace.Root.ContextDirs() for the context list; read work.md and journal.ndjson per context via their existing paths, e.g. Root.WorkPath(ctx) and the journal file next to it). (2) Find every occurrence of a legacy item id matching the exact pattern `D-<4 digits>` (regexp `\bD-\d{4}\b`) in those files — both in item headings/bodies of work.md and in journal.ndjson lines (the journal is NDJSON: rewrite the id inside the JSON values, do NOT reformat the JSON — a plain textual replacement of the id token is correct and keeps every other byte identical). (3) The replacement is the same number with the ADR prefix: ADR-0010 -> ADR-0010. (4) COLLISION GUARD: before writing anything, if any target id (ADR-<same digits>) already exists anywhere in the workspace, abort with a clear error naming the collision and change nothing. (5) DRY RUN BY DEFAULT: without -apply, print one dense line per planned rewrite — `m <file> ADR-0010 -> ADR-0010 x<count>` — plus a trailing `ok <n> ids in <m> files (dry-run; pass -apply to write)`, and exit 0 without modifying anything. With -apply, perform the rewrites and print `ok migrated <n> ids in <m> files`. (6) IDEMPOTENT: a second run finds nothing and prints `ok nothing to migrate`. Wire the subcommand into cmd/spectackle/main.go's run() dispatch exactly like the existing lint/reindex subcommands (they use rootFlag-style flag parsing and return an int exit code) and add it to the usage() text. Tests: internal/migrate/migrate_test.go over a fixture workspace built in a t.TempDir() containing (a) a root .spectackle/work.md with a live `## ADR-0010 some decision` item block, (b) a root .spectackle/journal.ndjson with lines referencing ADR-0010 and ADR-0011, (c) a nested context bundle with no D ids at all — assert: dry-run reports the right files/counts and changes no bytes; apply rewrites exactly the ids and leaves every other byte identical (compare the full file contents against the expected string); a second apply reports nothing to migrate; and a fixture where ADR-0010 already exists causes an error with no writes. Add one cmd-level test in main_test.go asserting run([]string{"migrate-adr", tmpdir}) returns 0 in dry-run. Verify: go vet ./internal/migrate/ ./cmd/spectackle/ && go test ./internal/migrate/ ./cmd/spectackle/ -race && go test ./... -race && make build && ./bin/spectackle lint . (must stay 15/61/0). IMPORTANT: do NOT run migrate-adr -apply against the real repo — the orchestrator does that deliberately after review. Report the dry-run output shape and the test list. Rollback: delete internal/migrate, revert main.go.

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
