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

## B-01KYMCJG8HFYWBSFVG10KJP3NR knowledge path= resolves against the process cwd, not -root
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/knowledge.go, docs

Found by the R-01KYMA7EXME6K gap hunt (FAIL 3 + WARN 10). knowledge op=export path=kb-export.md wrote into the directory the PROCESS was started from - during the hunt that was the spectackle source repo itself, not the -root workspace; the hunter deleted it and verified a clean tree. Same for apply path= (reads from cwd). For a tool whose stated principle is that -root is the workspace and the LLM never names host paths, an undocumented cwd exception is a hazard, and a long-lived MCP server has a fixed cwd unrelated to the -root it serves. FIX: resolve a RELATIVE path against -root (absolute paths keep working verbatim); say so in the field description and docs. Fold in WARN 10 while here: an empty or whitespace-only artifact is diagnosed as schema mismatch (schema != v1) - detect and say empty artifact. TEST: export path=x.md with a cwd elsewhere lands under -root; absolute path unchanged; apply reads the root-relative file; empty artifact refuses naming emptiness. VERIFY: go build ./... && go test ./... -count=1.
