---
schema: v0
---

## P-0069 M6 Vulkan host-binding resolver: SPIR-V module provenance -> ELaunch edges
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/resolve/gpupipe.go, internal/resolve/resolver.go

Last open M6 resolver deliverable. Metal is done (GpuPipeResolver, newFunctionWithName: name-match). Vulkan was parked because there is no host-side kernel-name literal: host code binds a compiled SPIR-V blob by FILE PATH via vkCreateShaderModule and names the entry point through VkPipelineShaderStageCreateInfo.pName, which is nearly always "main". T-0073 recorded the finding; P-0046 shipped the GLSL langspec so the target nodes now exist (glsl: nodes, 30th language). Only the host->module binding is missing.

Approach: module provenance as a per-file symbol table, not a global one.
1. Scan host sources (C, C++, Go) for a .spv path literal or a shader-blob identifier, remembering path stem.
2. vkCreateShaderModule(dev, &ci, NULL, &handle): bind handle identifier -> shader stem, sourced from the createInfo struct's pCode field which traces back to the blob loaded in step 1.
3. VkPipelineShaderStageCreateInfo with .module = handle and .pName = "entry": resolve handle -> stem, then look up the graph for a LangGLSL KFunc node whose Name == entry and whose File basename stem matches. Emit ELaunch from the enclosing host function to that node's actual ID (collision suffix preserved).

Critical constraint: GLSL is QualFlat, so every shader's void main() mints glsl:main and collides to glsl:main~2, glsl:main~3 in file-path order. The resolver must therefore NOT mint an ID by string concatenation the way the Metal slice does; it must query graph.Graph.Find and filter by Lang/Kind/Name/File to obtain the real node ID. This keeps RSV-001 (resolvers emit edges only, never nodes) intact and is the reason module provenance has to reach a file, not just an entry-point name.

Heuristic status per RSV-002: string/line matching, best effort, no edge when provenance cannot be traced end to end. Silence beats a wrong edge.

## T-0099 VulkanResolver: module-provenance line scanner + graph-lookup ELaunch edges
kind: task
state: active
created: 2026-07-24
parent: P-0069
targets: internal/resolve/vulkan.go, internal/resolve/vulkan_test.go, internal/resolve/resolver.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body before touching anything; do not explore beyond the files named here.

GOAL
Add the last open M6 resolver: Vulkan. Metal already works (GpuPipeResolver, internal/resolve/gpupipe.go) by matching a newFunctionWithName:@"kern" literal to an msl kernel of the same name. Vulkan has no such literal — the host binds a compiled SPIR-V blob by FILE PATH and names the entry point through VkPipelineShaderStageCreateInfo.pName, which is almost always "main". So the binding must be recovered by tracing module provenance across three statements inside one host file.

SCOPE (disjoint, lease exactly these)
  internal/resolve/vulkan.go       NEW
  internal/resolve/vulkan_test.go  NEW
  internal/resolve/resolver.go     one line: register VulkanResolver{} in Default()
Do NOT touch gpupipe.go, cuda.go, cgo.go, ffi.go, plan9asm.go or any file outside internal/resolve. The concurrently running sibling task owns docs/roadmap.md, docs/design-wasm-parsers.md and internal/mcpserver/decide.go — leave all three alone. .spectackle files are server-owned: never edit them by hand.

DESIGN
Implement BindingResolver (see internal/resolve/resolver.go):
  Name() string        -> "vulkan"
  Langs() []graph.Lang -> {graph.LangC, graph.LangCPP, graph.LangGo, graph.LangGLSL}
  Resolve(ctx, g graph.Graph, fs FileSet) ([]graph.Edge, error)

Per host file, one forward line scan with a tiny per-file symbol table. Reuse the enclosing-host tracking already proven in gpupipe.go: braceDelta plus cFunctionRe give you the currently open host function and its brace depth. Copy that shape rather than inventing a new one, but keep the code in vulkan.go — do not refactor gpupipe.go into shared helpers, that would collide with nothing today but adds churn for no gain.

Three provenance steps, all heuristic line matches:
  1. BLOB: a string literal ending in .spv (e.g. "shaders/blur.comp.spv", "blur.spv"). Record stem = basename with every extension stripped, so blur.comp.spv -> blur. Bind stem to the identifier being assigned on that line if there is one (code = readFile("blur.comp.spv") / auto code = ...; / const char *p = "blur.spv";). If the line has no assignment target, remember the stem as the file's most recent pending blob.
  2. MODULE: vkCreateShaderModule(dev, &ci, NULL, &mod). Bind the handle identifier — the last argument, with a leading & stripped — to a stem: prefer a stem bound to an identifier that appears in the preceding VkShaderModuleCreateInfo initialization (pCode = <ident>), else fall back to the file's most recent pending blob stem. Also accept the common one-liner where the createInfo struct literal and the call are on the same line.
  3. STAGE: a VkPipelineShaderStageCreateInfo initialization carrying .module = <handle> (or module: <handle> in Go bindings) and .pName = "entry" (or pName: "entry"). The two fields are usually on adjacent lines inside one struct literal, so accumulate fields across the literal's brace span rather than requiring one line. Default entry to "main" only when .module is present and .pName is absent — Vulkan itself does not default it, but omitting it in a struct literal in C zero-initializes to NULL and such code cannot run, so treating the absent case as "main" would invent an edge. Do NOT default: skip the stage when pName is absent.

TARGET RESOLUTION (the part that must not be shortcut)
Given (stem, entry), find the real node ID:
  cands := g.Find(entry, 64, graph.KFunc)
  keep n where n.Lang == graph.LangGLSL && n.Name == entry && stemOf(n.File) == stem
  stemOf strips the directory and every extension, matching step 1.
If exactly one candidate survives, emit the edge to n.ID. If zero survive, emit nothing. If more than one survives (same stem in two directories), emit nothing — ambiguity is not an edge.
This is why the resolver takes g: GLSL is QualFlat, so shader mains mint glsl:main and the indexer disambiguates to glsl:main~2, glsl:main~3 in file-path order. Never build the target ID with ids.Mint — that would produce glsl:main for every shader and wire the second shader's launches to the first shader's node. RSV-001 also forbids resolvers from minting nodes at all.

EDGE SHAPE
  graph.Edge{Src: <enclosing host node ID>, Dst: <resolved glsl node ID>, Kind: graph.ELaunch, File: hostPath, Line: <line of the stage struct>}
Src uses the same convention gpupipe.go uses for its language: for C/C++ hosts ids.Mint("c"/"cpp", host); for Go hosts the package-qualified go: ID. If you cannot determine the host function, skip — no file-level edges.
Dedupe per (src, dst, file) exactly like gpupipe.go's launchKeyGpu.

TESTS (internal/resolve/vulkan_test.go, table-driven, follow gpupipe_test.go's in-memory FileSet style)
  1. happy path: one .comp shader with void main() indexed into a memGraph, one C host with the three statements -> exactly one ELaunch edge to the right node ID.
  2. collision case, THE regression proof: two shaders blur.comp and sharpen.comp, both void main(), so the graph holds glsl:main and glsl:main~2. Two host functions each binding one of them. Assert each edge lands on the node whose File matches its own stem — a naive ids.Mint implementation fails this test, which is the point.
  3. incomplete provenance: vkCreateShaderModule with no traceable .spv blob -> zero edges.
  4. missing pName -> zero edges (no "main" default).
  5. ambiguous stem: same stem in two directories -> zero edges.
  6. determinism: run Resolve twice over the same FileSet, assert byte-identical edge slices in the same order (SPX-ARC-004 family; sort before returning if map iteration leaks in).

VERIFY (run all of these, all must pass, paste nothing that did not run)
  go build ./...
  go test ./internal/resolve/... -race
  go test ./... 
  go vet ./internal/resolve/...
  ./bin/spectackle lint
The resolver runs on this repo's own index, which contains no Vulkan host code, so full-repo indexing must stay byte-identical: after building, confirm reindex still reports the same node/edge counts (1349 nodes, 1665 edges) — a change there means the new resolver is emitting spurious edges.

EXIT CRITERION
All six tests green under -race, ./... green, lint clean, and repo index counts unchanged. Then move the task to done.

ROLLBACK
The change is two new files plus one registration line. Reverting means deleting vulkan.go and vulkan_test.go and dropping the r.Register(VulkanResolver{}) line; nothing else in the pipeline observes the resolver, no schema, no stored format, no anchors move.

HANDOFF
lease claim the three paths under this task id, work op=start, implement, run the verify block, work op=submit, then lease release. Report which tests you ran and their real output.
