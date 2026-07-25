---
schema: v1
---

## intent
- P-01KY8ZHMMRE0RBJQMTA4PH5WDB CUDA chain live: CudaParser + launch edges - saxpy triple-hop reproduces: cuda chain live: CudaParser + launch edges, saxpy triple-hop reproduces
- T-01KYAWGR68FRNB4Z6S6K2J6VBP VulkanResolver: module-provenance line scanner + graph-lookup ELaunch edges: VulkanResolver complete. The first implementer died leaving vulkan.go non-compiling (unused variable) and no test file at all; the continuation removed the dead variable, added six tests and found no contradiction with RSV-001/003/004. Resolver contributes zero edges on this repo, which has no Vulkan host code.
- P-01KYAWDTE8FPXSMZRV5M0D7GD2 M6 Vulkan host-binding resolver: SPIR-V module provenance -> ELaunch edges: delivered by T-01KYAWGR68FRNB4Z6S6K2J6VBP. Last open M6 resolver.

## RSV-001 {applies: go:resolve.Default}
The Binding resolvers SHALL only add cross-language edges between nodes the parsers minted — `resolve.BindingResolver` implementations never mint `graph.Node` values and never mutate parser-owned fields.

## RSV-002 {applies: go:resolve.GpuPipeResolver.Resolve}
WHEN an Objective-C host body contains a newFunctionWithName string literal, the gpupipe resolver SHALL emit one deduplicated ELaunch edge from the enclosing host symbol to the msl kernel of that name and never mint or mutate nodes.

## RSV-003
WHEN a host file binds a SPIR-V shader path through vkCreateShaderModule to a VkPipelineShaderStageCreateInfo pName, the vulkan resolver SHALL emit one ELaunch edge to the single glsl node found by graph lookup, never by minting an ID.

Rationale: GLSL is QualFlat: shader mains collide to glsl:main~N in file order, so only a lookup filtered by name and file stem yields the right target. RSV-001 forbids minting.

## RSV-004
IF the chain from shader file to entry point is incomplete or resolves to more than one glsl node, THEN the vulkan resolver SHALL return zero `graph.Edge` values for that dispatch.

Rationale: Silence beats a wrong cross-language edge; ambiguity is not a binding.