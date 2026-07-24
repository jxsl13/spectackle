---
schema: v0
---

## P-0027 find/get node records show the definition span — start AND end line
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: go:graph.Node

Requirement: search output shows where functions begin but not where they end. graph.Node.EndLine exists (anchor spans use it) but the n-record renderer emits only file:Line. Fix: n records render file:Line-EndLine when EndLine>0 (e.g. internal/graph/graph.go:210-250), unchanged single line when EndLine==0. Token cost: ~4 chars/record — well inside SPX-ARC-002 budgets; every consumer (impact packs, find, get, research) inherits the span for free. Update docs/tools.md grammar line for n records.

## T-0049 n records render file:start-end span
kind: task
state: approved
created: 2026-07-24
parent: P-0027

SCOPE: the single function in internal/mcpserver that renders n node records (find it via spectacle: find q=nodeRec / inspect tools.go around getNode/find rendering; it formats '<file>:<line>'), its test file, and docs/tools.md (grammar section, n-record line only).

CHANGE (MCP-002 rule family): when n.EndLine > n.Line, render '<file>:<Line>-<EndLine>' (e.g. internal/graph/graph.go:210-250); when EndLine==0 or ==Line, keep '<file>:<Line>' exactly as today. Apply in EVERY place n records are rendered if there is more than one formatter — grep-free discovery via spectacle find scope=code q=records; consolidate to one helper if trivially possible without touching unrelated code.

TESTS: unit test with two nodes (EndLine set / unset) asserting both forms in find output over the wire (connectRoot pattern in tools_test.go); adjust ANY existing assertions that match the old single-line form if they break (they assert Contains, so most survive).

ROLLBACK: single revert; format-only change.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/mcpserver/ green; docs/tools.md grammar updated. Constraints: never edit .spectacle/ (server-owned); never commit/push; scope is the renderer + its tests + one docs line — nothing else.
