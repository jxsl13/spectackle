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

## P-0081 README: a mermaid diagram of the whole flow and a reference for every persisted structure
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: README.md

Two documentation gaps that cost every newcomer, human or agent, the same rediscovery.

First: nothing shows the flow. The lifecycle (research, draft, grill, decide, approve, fan out, check, archive), the orchestrator/implementer split with worktrees and leases, and the backprop path from archive into spec.md plus anchors all exist as prose spread over README, docs/agent-workflow.md and the instructions manifest. A reader has to assemble the picture. A mermaid diagram renders natively on the forge, costs nothing to keep next to the prose, and is the one artifact that makes the state machine and the agent topology legible at a glance.

Second: the persisted structures are undocumented as a set. Each file is described where it happens to be mentioned, never together, and never with the reason it exists as its own file rather than a field of another. That reason is the interesting part: journal.ndjson is append-only because rejections must survive compaction; work.md holds ACTIVE items only because archived ones would grow it without bound; anchors.tsv is separate from spec.md because rule text and code position drift independently and the two-hash design depends on that separation; the cache is derived and gitignored because it must be rebuildable from the versioned files alone.

Scope note: this documents what exists TODAY, including the knowledge artifact format that just landed. It does not wait for the knowledge tool, which is being built in parallel and adds no new persisted structure — the artifact is a portable interchange file, not workspace state.

Rejected: a separate docs/ page. The data-structure reference belongs where someone first meets the repository, and the README already carries the quickstart and the headless recipes; splitting it would leave the README describing a system whose state nobody can see.

## T-0112 README: mermaid flow diagram and a reference for every persisted structure
kind: task
state: active
created: 2026-07-24
parent: P-0081
targets: README.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This is a documentation task: no Go code changes at all.

SCOPE (lease exactly one file)
  README.md
Do NOT touch docs/, internal/, cmd/, AGENTS.md, .claude/ or .github/ — two sibling tasks own internal/mcpserver and the command generator right now. .spectackle files are server-owned: never edit them by hand.
Do not restructure the README or move existing sections. Both additions go in as NEW sections: the diagram directly after the short explanation near the top (before or right after Quickstart, wherever it reads naturally), the data-structure reference further down.

RESEARCH FIRST, then write. Everything below must describe what the code ACTUALLY does. Read, do not assume:
  internal/workspace/workspace.go   the bundle layout, SchemaStamp, Config (Ignore, IgnoreRegex, Compact), scaffolding
  internal/item/item.go             work.md format, Item fields incl. Parent/Needs/Refs and the ADR fields
  internal/journal/journal.go       journal.ndjson, append-only, event kinds
  internal/drift/drift.go           anchors.tsv columns, CHash vs RHash, the classification table
  internal/spec/cascade.go          spec.md, front matter, cascade/override/scope resolution
  internal/knowledge/artifact.go    the portable artifact (NOT workspace state — say so)
  internal/lifecycle/lifecycle.go   the state machine and its ordering
  internal/coord (coord.db)         leases, agent registry, ID counters
  docs/agent-workflow.md, docs/lifecycle.md, docs/spec-cascade.md for prose that already exists
If a claim you want to make is not verifiable from the code, leave it out. A wrong README is worse than a thin one.

PART 1 — THE DIAGRAM
One mermaid diagram (the forge renders it natively; use a fenced ```mermaid block). It must make three things legible at a glance, and it is fine to use subgraphs to keep them in one picture:
  a) the lifecycle path: research, draft, grill, decide, approve, fan out, check, archive — and the state machine's ordering draft < submitted < approved < active < done < archived, plus rejected (revocable) and blocked (server-only side state, exits via decide: rescope, reject, override-once).
  b) the agent topology: a strong orchestrator that drafts and reviews, cheap implementers each in their own git worktree holding leases over disjoint scope, and the submit path back to main.
  c) the backprop path: archive merges the delta into spec.md, check stamps anchors.tsv, drift classification feeds the next round.
Keep it readable. If one diagram becomes unreadable, use two, but say why in a sentence between them. Verify the syntax parses rather than eyeballing it — a broken mermaid block renders as an error box on the forge, which is worse than no diagram.

PART 2 — THE DATA-STRUCTURE REFERENCE
One section covering EVERY persisted structure. For each: where it lives, its format, what it holds, and — the part that matters most — WHY it is its own file rather than a field of something else. Cover at least:
  .spectackle/spec.md        EARS rules + prose sections; cascades by directory; front matter (schema, prefix, scope, inherits, overrides)
  .spectackle/work.md        ACTIVE items only. Say why: archived items would grow it without bound, and the journal already carries their history.
  .spectackle/journal.ndjson append-only event log. Say why append-only: rejections must survive compaction, which is what makes the rejection corpus trustworthy.
  .spectackle/anchors.tsv    rule-to-node bindings with two hashes. Explain CHash vs RHash and why they are separate: code position and rule text drift independently, and the whole four-way classification (ok/moved, evolved, tightened, diverged) exists only because both axes are tracked. Note that only the evolved quadrant is ever auto-healed, and why.
  .spectackle/config.yaml    ignore globs, ignore_regex, compact thresholds; auto-generated with defaults
  .spectackle/cache/         derived, gitignored, rebuildable from the versioned files alone — say that this is the invariant that makes it safe to delete
  coord.db                   cross-process coordination: agent registry, scope leases, global ID counters. Say why SQLite/WAL rather than a file: multiple server processes write it concurrently, and a plain file is a read-modify-write race.
  the knowledge artifact     portable interchange between repositories, NOT workspace state. Make that distinction explicit.
Also state the two cross-cutting invariants, because they explain the shape of everything above: the schema stamp rotates on format breakage and there is no migration mechanism anywhere; and the LLM never writes these files — every capability is a server-side write path.

VERIFY
  Confirm the mermaid block parses. If no renderer is available, at minimum check the syntax carefully against the mermaid grammar and say in your report how you validated it.
  go build ./... && go test ./...   (must still pass — you changed no code, this is the guard that you did not)
  /home/user/spectackle/bin/spectackle lint
  Re-read your data-structure section against the source files and confirm every claim.

EXIT CRITERION
Both sections present, the diagram parses, every structural claim traceable to code you read, tests and lint unchanged and green, and no file other than README.md modified.

ROLLBACK
Documentation only. git checkout README.md restores the prior state; no code, schema, record or anchor is touched.

REPORT BACK
How you validated the mermaid syntax, the list of source files you read to ground the reference, any claim you WANTED to make but dropped because the code did not support it, and anything you deliberately did NOT do.
