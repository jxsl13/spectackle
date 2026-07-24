# Metal compute example — the ObjC → Metal chain, live

This example is an index fixture (not a buildable app) demonstrating the
ObjC/Metal analog of the saxpy Go → C → CUDA chain: `dispatch` reaches
`buildPipeline` via an Objective-C message-send edge (objcSpec CallRe,
LSP-002), and `buildPipeline` reaches the `add_arrays` kernel via the
gpupipe string-binding launch edge (`newFunctionWithName:@"add_arrays"`,
RSV-002). The bundle's contracts (MTC-API-004, MTC-API-006) are
server-authored and drift-anchored to both ends of the chain.

Real output of `get {"id":"objc:Renderer.dispatch","depth":2}` against the
shipped binary on this repo:

```
n objc:Renderer.dispatch method examples/metalcompute/Renderer.m:17-20
n objc:Renderer.buildPipeline method examples/metalcompute/Renderer.m:12-15
n msl:add_arrays kernel examples/metalcompute/shaders.metal:4 sig=device const float* a [[buffer(0)]], device float* out [[buffer(1)]], uint i [[thread_position_in_grid]]
e objc:Renderer.dispatch call objc:Renderer.buildPipeline via=examples/metalcompute/Renderer.m:18
e objc:Renderer.buildPipeline launch msl:add_arrays via=examples/metalcompute/Renderer.m:13
r MTC-API-004 U examples/metalcompute The Renderer SHALL bind the kernel by the literal name add_arrays through newFunctionWithName.
r MTC-API-006 E examples/metalcompute WHEN add_arrays is dispatched, the compute pipeline SHALL process exactly one index per `thread_position_in_grid` so `out[i]` depends only on `a[i]`.
r-root SPX-ARC-001 SPX-ARC-002 SPX-ARC-003 SPX-ARC-004 SPX-ARC-005 SPX-REPO-001 SPX-REPO-002 SPX-TST-001 SPX-SWM-001 SPX-SWM-002 SPX-SWM-003 SPX-SWM-004 SPX-SWM-005 SPX-SWM-006 SPX-SWM-007 SPX-ARC-006
```
