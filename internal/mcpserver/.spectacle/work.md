---
schema: v0
---

## P-0017 state tool + /spectacle-state prompt: one structured, read-only full picture
kind: proposal
state: approved
created: 2026-07-24
targets: internal/mcpserver/tools.go, go:mcpserver.Server.check

One call answers 'where does spec-driven development stand on this repo': sectioned dense records #version #items #rules #graph #swarm #drift #health - item counts by state plus i lines, rule totals per context dir plus lint findings, graph node/edge stats, ag/l/wt swarm view, anchor drift classification WITHOUT side effects (strictly read-only, unlike check), compact-due and coverage summary. Same text exposed as MCP prompt 'state' so clients surface it as a slash command (/mcp__spectacle__state). Budget-truncated per SPX-ARC-002.

## T-0029 implement state tool + prompt (read-only, sectioned)
kind: task
state: draft
created: 2026-07-24
parent: P-0017

Scope ONLY: new internal/mcpserver/state.go + state_test.go; internal/mcpserver/tools.go (ONE AddTool block in registerTools); internal/mcpserver/prompts.go (add 'state' prompt reusing the shared builder); internal/graph/graph.go + graph_test.go (add Stats() (nodes, edges int) to Graph interface + memGraph impl + test); docs/tools.md (tool section 11 'state', header tool count 10->11, prompts section lists state). Input stateIn{Path string subtree default all; Budget int default 2000}. Shared builder (s *Server) stateText(path string) (string, error) used by tool handler (via gate) AND prompt handler (locks s.mu + scan.Refresh itself, pattern in prompts.go). Sections, all dense-line, omit empty ones (SPX-MCP-004 spirit): #version -> ok spectacle <Version> agent <s.agent> root marker; #items -> ok items total=N active=a approved=b draft=c submitted=d done=e, then i lines (item.LoadAll filtered by within(path, it.Dir), non-draft first); #rules -> ok rules total=N dirs=M findings=F then per dir 'ok dir <dir> rules=n' (spec.Load + All(), findings count from c.Findings(), list ! lines only if F>0); #graph -> ok graph nodes=N edges=M (new g.Stats()); #swarm -> ag/l/wt lines (cd.Agents, cd.Leases(agentTTL), cd.Worktrees); #drift -> drift.Load + drift.Classify with the same rule-exists func as check BUT ABSOLUTELY NO writes: no drift.Save, no backprop, no journal - Moved anchors are just counted as moved; summary 'ok anchors total=N ok=x pending=y moved=z' plus d lines for changed/gone/stale; #health -> s.compactCandidates(path) c lines + 'ok coverage gaps=N' via a count-only variant of coverageGaps (reuse it, len()). Read-only contract test: capture mtimes of .spectacle files + journal length before/after state call -> unchanged. Budget: budget.TruncateRecords over all lines. Add EARS rule via driver: rule op=add dir=internal/mcpserver pattern=E system='the server' trigger='the state tool is invoked' response='report items, rules, graph, swarm, drift and health as sectioned dense records while writing no file' applies=['go:mcpserver.Server.state'] item=P-0017 (run AFTER implementing so the anchor resolves; expect ok SPX-MCP-005). Tests: connectRoot pattern; assert sections present on a seeded workspace, absent when empty; read-only assertion; prompts/get state returns same sections. Verify: go build ./internal/... && go vet ./internal/mcpserver/ ./internal/graph/ && go test ./internal/mcpserver/ ./internal/graph/ -race green.
