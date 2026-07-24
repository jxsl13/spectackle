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

## T-0051 ddnet validation: index the real thing, measure, prove a cross-language chain
kind: task
state: approved
created: 2026-07-24
parent: P-0026

SCOPE: docs/validation-ddnet.md (NEW — the only repo file you create/edit). Read-only use of bin/spectacle and the ddnet clone at /tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/ddnet (shallow clone, ~2300 C/C++ files under src/).

STEPS:
1. Build: go build -o bin/spectacle ./cmd/spectacle (repo root /home/user/spectacle).
2. Index ddnet via the MCP driver against the CLONE root (this scaffolds a .spectacle folder inside the clone — that is fine, it is a scratchpad):
   SPECTACLE_AGENT=agent-ddnet python3 /tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/mcp_call.py /tmp/claude-0/-home-user-spectacle/4c40537b-65eb-5824-86a6-6c853d4e1c78/scratchpad/ddnet <<'JSON2'
   {"name":"state","arguments":{}}
   JSON2
   Record: node count, edge count, and the wall-clock of the first (cold) and second (warm parse.db) run — time the driver invocations with `time`.
3. Probe REAL symbols over the driver (find scope=code + get depth=2): pick 3 well-known ddnet functions (e.g. find q=CServer, q=str_copy, q=net_init — base/system.c is the C layer, src/engine/server is C++). Capture: (a) one C function with in-edges from C++ callers (the FFI/same-name bridge or direct c: calls), (b) one n record showing a file:start-end span, (c) one get id=<cpp fn> depth=2 impact pack crossing into c: nodes.
4. Write docs/validation-ddnet.md (<=120 lines, dense): repo+commit of the clone, measured numbers (nodes/edges/cold/warm/graph memory if visible from state), the three captured probes as verbatim record lines, what the regex chain does NOT see (member calls via ->, virtual dispatch, macros — honest limits section), verdict sentence.
5. Lifecycle: lease claim paths=["docs/validation-ddnet.md"], move active, write, move done, release.

EXIT CRITERION: docs/validation-ddnet.md exists with real measured numbers (no placeholders); the three probes show at least one c:<name> node with File/Line and at least one edge crossing cpp:->c: or c: in-edges from another file. Constraints: do NOT modify the ddnet clone besides its scaffolded .spectacle; do NOT touch any Go source; never commit/push.
