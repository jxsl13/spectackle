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

## P-0093 docs drift again: install-hooks undocumented, and agent-workflow knows none of the last six features
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: docs/tools.md, docs/agent-workflow.md

SPX-REPO-001 requires docs/tools.md to track the Go structs. It drifted again within one round: commands gained an install-hooks op that the document does not mention at all. That is the second occurrence, which makes it a pattern rather than an oversight — every task whose lease excludes docs leaves a gap behind, and nothing collects them.

docs/agent-workflow.md is worse off and under no contract at all. It is the document the orchestrator/implementer division of labor is defined in, and it knows nothing about six things that shipped since: the claimable-queue records that answer what an orchestrator can start right now, item citations, the generated pre-commit hook that keeps records off worktree branches, the rebuild-and-restart command, the stale-binary hint, and the default-versus-explicit command generation. An agent following that document as written would hand-derive the claimable set and would not know the hook exists.

Measured: zero mentions of q free, q held, refs, install-hooks, make dev or the stale hint in that file.

Rejected: putting agent-workflow under SPX-REPO-001 as well. That contract is about schema-to-document consistency, which is mechanically checkable; prose about how agents divide labor is not, and a contract nobody can verify is worse than none.

Rejected: folding both documents into one task per file. They drift for the same reason and are read together; splitting them would double the reading of the same source code.

## T-0127 docs: install-hooks in tools.md, six shipped features into agent-workflow.md
kind: task
state: active
created: 2026-07-24
parent: P-0093
targets: docs/tools.md, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This is a documentation task: no Go code changes at all.

SCOPE (lease exactly these two)
  docs/tools.md
  docs/agent-workflow.md
Do NOT touch internal/, cmd/, Makefile, README.md or any other docs file. .spectackle files are server-owned: never edit them by hand.

RESEARCH FIRST. Every claim must be true of the code as written. Read the source, do not trust this brief's summaries — where they differ from the code, the code wins and you say so in your report.

PART 1 — docs/tools.md: the install-hooks op
SPX-REPO-001 requires this file to match the Go structs. commands gained an op the document does not mention (measured: zero occurrences). Read internal/mcpserver/commands.go for commandsIn's op enum and the install-hooks implementation, and internal/mcpserver/templates/hooks/pre-commit.tmpl for what the hook does.
Document, in the existing commands section: the op, where it writes (the repository's COMMON git dir, so one install covers every worktree), the 0o755 mode and why it matters (git silently ignores a non-executable hook), the refusal when a foreign pre-commit hook exists without the generated marker, and the success record shape. Add the success record to the grammar table if it is a new record kind.
Also document what the hook enforces: a commit inside a LINKED worktree that stages any .spectackle path is rejected; code-only worktree commits and every commit in the main checkout pass. Name the reason — .spectackle is server-owned and SPX-SWM-001 already confines agent branch commits to code — and the two limits: hooks are not versioned, and --no-verify bypasses them.

PART 2 — docs/agent-workflow.md: six features it predates
This file defines the orchestrator/implementer division of labor and knows none of the following (measured: zero mentions of each). For each, read the source before writing, then work it into the section where it belongs rather than appending a list at the end:
  1. swarm's claimable queue (internal/mcpserver/swarm.go): q free and q held records. This changes the fan-out section materially — the document currently implies the orchestrator derives the claimable set itself, and it no longer has to.
  2. item citations (internal/item/item.go Refs, internal/mcpserver/tools.go draftIn): what refs mean versus parent and needs, and that unknown ids are refused at the write path.
  3. the generated pre-commit hook: why an implementer's commit must not carry .spectackle paths.
  4. make dev (Makefile): the repository develops itself with itself, so the resident server must be rebuilt and restarted after every merged change.
  5. the stale-binary hint (internal/mcpserver/swarm.go): what it means when an agent sees it.
  6. default versus explicit command generation (internal/mcpserver/commands.go): three commands by default, the exploration set on request.
Also check the grill section: grill gained a deliberation question for proposals. Document it as a question and not a gate, because that distinction is the whole design.

WHAT NOT TO DO
Do not document anything unshipped. If you find something in this brief that the code does not do, report it instead of writing it.
Do not restructure either document. Both have a settled shape; additions go where their neighbors already are.

VERIFY
  go build ./... && go test ./...    (must still pass — you changed no code; this is the guard that you did not)
  /home/user/spectackle/bin/spectackle lint
  For every claim you write, name the file you read it from in your report.

EXIT CRITERION
install-hooks and the hook's rule documented in tools.md; all six features plus the grill question worked into agent-workflow.md; nothing unshipped documented; no file other than the two leased ones modified; tests and lint unchanged and green.

ROLLBACK
Documentation only. git checkout of the two files restores the prior state; no code, schema, record or anchor is touched.

REPORT BACK
For each item, the source file you verified it against and anything the code does differently from this brief, whether any grammar-table row was needed, and anything you deliberately did NOT do.
