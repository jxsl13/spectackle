---
schema: v1
---

## B-01KYMCHPF6EXZ9FHGZFX5ZNSHK find scope=rule omits the pattern field the documented r-line grammar requires
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/tools.go

Found by the R-01KYMA7EXME6K gap hunt (FAIL 2), field-count verified in two fixture repos. docs/tools.md line 47 documents r <ruleID> <P> <scopeDir> <text>; rule op=add and get both render the pattern, find scope=rule does NOT: find renders r AUTH-002 src/auth The auth module SHALL... A caller parsing per the documented grammar reads scopeDir as the pattern and the sentence first word as scopeDir. FIX: the find rule renderer emits the pattern like the other two paths (one shared ruleLine helper if they have diverged). TEST: pin that find scope=rule and get render the same field count and the same pattern token for the same rule. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.

## B-01KYMCJFC3EMNBBP3T1S1AXD66 draft targets given as node IDs never resolve their context dir; path targets do
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/tools.go

Found by the R-01KYMA7EXME6K gap hunt (FAIL 4), isolated in a dedicated fixture. docs/tools.md sec 3 states the server assigns the context dir as targets -> deepest common context, else root. It holds for PATH targets and fails for NODE-ID targets: with pkg/foo an unambiguous context dir (its own spec.md), draft targets=[go:foo.Frobnicate] lands at ROOT, while draft targets=[pkg/foo/foo.go] and draft dir=pkg/foo both land at pkg/foo. Node IDs are the advertised currency (the LLM names concepts, never file paths), so the bug hits the primary path. DOWNSTREAM: such an item archives its delta into ROOT ## intent instead of the subsystem, which is also why every exported knowledge intent entry carried dir empty regardless of subject (gap-hunt WARN 9) and why those entries leak repo-local item IDs and paths into portable artifacts. FIX: the dir-derivation resolves node-ID targets through the graph to their file (the same resolution normalizeTargets and the scope guard already do) before computing the deepest common context. TEST: a fixture with a nested context dir - a node-ID target and the equivalent path target land in the SAME dir; pin that an unresolvable node ID still falls back to root. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.
