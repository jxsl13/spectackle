---
schema: v1
prefix: MTC
---
## MTC-API-004 {applies: objc:Renderer.buildPipeline}
The Renderer SHALL bind the kernel by the literal name add_arrays through newFunctionWithName.
## MTC-API-006 {applies: msl:add_arrays}
WHEN add_arrays is dispatched, the compute pipeline SHALL process exactly one index per `thread_position_in_grid` so `out[i]` depends only on `a[i]`.