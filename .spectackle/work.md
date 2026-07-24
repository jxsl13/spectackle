---
schema: v0
---

## D-0003 Rebranding spectacle→spectackle: wie tief? Der Go-Modulpfad (github.com/jxsl13/spectacle) muss zum GitHub-Repo-Namen passen — ein kompletter Modulpfad-Rebrand setzt voraus, dass du das Repo auf GitHub in spectackle umbenennst. Der .spectacle-Workspace-Ordner ist zudem das persistierte Format bestehender Nutzer-Repos.
kind: decision
state: done
created: 2026-07-24

kind: radio
option: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um
option: brand — Binary spectackle + MCP-Servername + Docs/README/goreleaser; Modulpfad und .spectacle-Ordner bleiben (nicht-brechend)
option: brand+dir — wie brand plus .spectackle-Ordner mit Legacy-Fallback; nur Modulpfad bleibt
choice: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um

## P-0046 M6 un-defer Vulkan parser side: GLSL shader langspec (30th language) via the cookbook
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

Requirement 'nix deferred': the gpupipe stub lists Vulkan as future because no shader source is parsed. First clean slice: a GLSL langspec parser so .comp/.vert/.frag/.geom/.tesc/.tese/.glsl shader entry points and functions become graph nodes (glsl:<name>), exactly like metalSpec did for MSL. This un-defers the PARSER side. NOTE recorded for the resolver side (do not fake it): unlike Metal's newFunctionWithName:@"kernel" direct name-match, Vulkan host code binds a compiled SPIR-V module blob by filename with pName almost always "main" — there is no host-side kernel-NAME literal to match against a glsl entry point, so a meaningful Vulkan launch-edge resolver is a genuinely harder, separate problem (module-provenance tracking), not a line-scanner analog; this proposal ships the parser and documents that finding in the gpupipe doc + roadmap rather than inventing edges. Follows docs/cookbook-new-language.md exactly. Exit criterion (explicit): glsl_test.go proves a real .comp file through index.New+IndexAll mints glsl:main and glsl:<helper> KFunc nodes, and non-def lines (control flow, calls, prototypes) mint nothing; full suite -race green; lint clean; cover>=70. Scope (disjoint, T-A): internal/graph/graph.go (LangGLSL const), internal/index/langs.go (7 shader exts), internal/langspec/glsl.go + glsl_test.go (new). Separate follow-up task T-B (disjoint file): gpupipe.go doc + docs/roadmap.md record the Vulkan-resolver finding. Rollback: revert additive files.

## T-0073 gpupipe + roadmap: record the Vulkan-resolver finding
kind: task
state: draft
created: 2026-07-24
parent: P-0046

Scope (disjoint from T-A, two files): internal/resolve/gpupipe.go (doc comment only — NO code change), docs/roadmap.md (M6 row annotation only). EDIT 1 — gpupipe.go: in the GpuPipeResolver doc comment, replace the Vulkan bullet '(still future)' text with a precise finding: Vulkan host code binds a compiled SPIR-V module blob by FILE PATH via vkCreateShaderModule and names the entry point through VkPipelineShaderStageCreateInfo.pName, which is almost always the literal "main" — so unlike Metal's newFunctionWithName:@"<kernelName>" there is no host-side kernel-name literal to string-match against a glsl entry point; a meaningful Vulkan launch edge needs module-provenance tracking (which .spv/.comp compiles into which module handle), a separate harder problem than the line-scanner name-match this resolver does for Metal. GLSL shader SOURCE is now parsed (glsl: nodes, P-0046) so the target nodes exist; only the host->module binding is unresolved. EDIT 2 — docs/roadmap.md: in the M6 row parenthetical, change the Vulkan mention to note GLSL source is parsed (30 languages) and that the Vulkan host-binding resolver remains open as module-provenance work (not a name-match analog). Keep wording tight, ~1 line each. Exit criterion: gpupipe.go still compiles (doc-only change: go build ./internal/resolve/ green); grep gpupipe.go for 'module-provenance' hits; roadmap M6 row mentions GLSL/30 languages. Verify: go build ./... && ./bin/spectackle lint . (after make build). NOTE: this task depends on T-A having shipped glsl: nodes — do it AFTER T-A is merged. Rollback: git checkout the two files.
