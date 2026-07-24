---
schema: v0
---

## intent
- P-0008 CUDA chain live: CudaParser + launch edges - saxpy triple-hop reproduces: cuda chain live: CudaParser + launch edges, saxpy triple-hop reproduces

## RSV-001 {applies: go:resolve.Default}
The Binding resolvers SHALL only add cross-language edges between nodes the parsers minted — `resolve.BindingResolver` implementations never mint `graph.Node` values and never mutate parser-owned fields.