---
schema: v0
---

## P-0026 langspec call edges + C/C++ FFI resolution — validated on ddnet
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: go:langspec.SpecParser.Parse, go:resolve.Default, internal/langspec/c.go, internal/langspec/cpp.go

Requirement: understand github.com/ddnet/ddnet (large C/C++/FFI codebase) — resolve calls into other languages analogously to Go. Today SpecParser emits ZERO edges (by design, TestSpecParserNoEdges); C/C++ nodes exist but have no call structure, so impact BFS dead-ends at language boundaries reached via langspec nodes.
Design, two slices + validation:
(1) FRAMEWORK: langspec.Spec gains optional call extraction for brace languages — body spans via brace counting from each Def hit, a CallRe (default: \b([A-Za-z_]\w*)\s*\( minus language keyword stoplist) applied inside the span, emitting ECall edges src=<lang>:<qual> dst=<lang>:<callee> with File/Line at the call site (same convention-based approach GoParser used pre-M3). Dst may be a nonexistent node (dangling edge) exactly like Go's syntactic pass — Impact tolerates it.
(2) FFI RESOLVER: new resolve/ffi.go — unifies the C family: cpp:X call edges whose dst has no cpp: node but a c:X node exists are remapped to c:X (extern "C"/header declarations); same for c:→cpp: mangling-free cases. Registered in resolve.Default(). RSV-001 holds: edges only, no node minting.
(3) VALIDATION on the ddnet clone (scratchpad): index, measure nodes/edges/latency, demonstrate a real cross-file C++→C chain via get depth=2; write docs/validation-ddnet.md with actual numbers.
Rollback: framework flag-off = no CallRe set means zero behavior change for all 25 non-C languages initially.

## T-0048 langspec call edges (CallRe framework) + C/C++ enable + FFI resolver
kind: task
state: done
created: 2026-07-24
parent: P-0026

SCOPE (disjoint from all other open work): internal/langspec/langspec.go, langspec_test.go, c.go, c_test.go, cpp.go, cpp_test.go, internal/resolve/ffi.go (NEW), internal/resolve/ffi_test.go (NEW), internal/resolve/resolver.go (ONLY the Default() registration line).

PART 1 — framework (LSP-002): extend langspec.Spec with optional fields `CallRe string` (call-site regex, capture group 1 = callee name) and `Stop []string` (language keywords never minted as callees: if, for, while, switch, return, sizeof, defined...). In SpecParser.Parse: for each Def hit that is a KFunc/KMethod, compute the body span by brace counting from the def line ({ depth tracking; body ends when depth returns to 0; single-line bodies ok; specs whose language has no braces simply leave CallRe empty). Apply CallRe to every line inside the span; for each callee c not in Stop and != the def's own name, emit graph.Edge{Src: <def node ID>, Dst: ids.Mint(lang, c) as NodeID, Kind: graph.ECall, File: path, Line: <call line>}. Set Node.EndLine from the brace-counted body end for CallRe-enabled languages (today SpecParser leaves EndLine 0 — check and keep other languages unchanged). UPDATE TestSpecParserNoEdges: it currently asserts zero edges by design — reshape it to 'no edges when CallRe is unset' plus a new sibling test proving edges WITH CallRe. All 25 existing languages keep CallRe unset in this task except C/C++.

PART 2 — C/C++ enable: cSpec and cppSpec get CallRe `\b([A-Za-z_]\w*)\s*\(` and a Stop list (C keywords + common macros: if, for, while, switch, return, sizeof, defined, static_assert, alignof, typeof). Tests: fixture where kernel_launch() calls helper() and printf() — assert the two ECall edges with exact src/dst/line; negative: `if (`, `sizeof(`, own recursive name.

PART 3 — FFI resolver (RSV-001 compliant): new internal/resolve/ffi.go, type FFIResolver, registered in resolve.Default() AFTER CgoResolver. Behavior: collect all ECall edges whose Dst node does not exist in the graph; if Dst is cpp:<name> and c:<name> exists, emit a NEW edge Src→c:<name> kind ECall (the dangling one stays — resolvers never mutate); mirror for c:<name>→cpp:<name>. Use g.Node() lookups only; NO node minting. Tests: graph with cpp:engine.Render calling dangling cpp:str_copy while c:str_copy exists → resolver adds cpp:engine.Render→c:str_copy; no-op when the real node exists.

ROLLBACK: CallRe unset = zero behavior change; single revert restores.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ ./internal/resolve/ ./internal/index/ green; make lint-specs clean. Constraints: never edit .spectacle/ (server-owned); never commit/push; do not touch mcpserver, graph, index beyond reading.
