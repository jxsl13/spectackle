---
schema: v0
prefix: SPX
---

# spectacle — living spec (root scope)

## intent
spectacle is a token-efficient, spec-driven MCP server: cross-language AST
maps, cascading EARS contracts and a git-native spec lifecycle, fused so an
LLM plans against structure and contracts instead of file contents.
- P-0002 dedicated unit tests with end-to-end coverage for every implementation: every package now carries a dedicated end-to-end unit test; concurrency race in draft found and fixed with a server mutex + regression test
- P-0001 M1 structural core: go/parser indexer + cgo edges: go/parser indexer live: 3-layer saxpy chain reproduces, anchors real (span+hash), find/get graph-backed
- P-0005 streamable HTTP transport: spectacle as resident localhost service: serve -http: Streamable HTTP via official SDK handler, resident localhost service; stdio default unchanged
- P-0006 orchestrator + fresh cheap subagents: document and embed the swarm workflow: orchestrator + fresh cheap implementer workflow documented (README, docs/agent-workflow.md) and embedded in server instructions
- P-0007 Plan 9 asm chain live: AsmParser nodes + go<->asm EAsm edges: plan9 asm chain live: internal/plan9 scanner pkg, AsmParser nodes, EAsm edges
- P-0009 forward-skip state machine: every forward jump is one move call: forward-skip state machine: total order, any forward jump one call, Mermaid automaton in README

## SPX-ARC-001
The spectacle server SHALL write only JSON-RPC 2.0 frames to stdout and route all log output to stderr.

Rationale: a single stray print corrupts the stdio MCP stream.

## SPX-ARC-002
WHEN a tool result exceeds the requested token budget, the server SHALL truncate the result at a record boundary and append a final `cur` record containing a resume cursor.

Rationale: token efficiency is the product; truncation must never split a record.

## SPX-ARC-003
IF a tool receives a symbol ID that is not present in the graph, THEN the server SHALL return the 3 closest matching IDs marked with an `nf` record instead of a JSON-RPC error.

Rationale: an error round-trip costs the caller more tokens than a correction.

## SPX-ARC-004
The input schema of every MCP tool SHALL declare a `default` value for each optional parameter.

Rationale: defaults let the LLM omit parameters, shrinking every call.

## SPX-ARC-005
The server SHALL confine every file write to `.spectacle` folders and keep the `.spectacle/cache` directory out of git via a server-written .gitignore file.

Rationale: the workspace belongs to the user; only the spec knowledge base is server territory, and caches must never be versioned.

## SPX-REPO-001
The repository SHALL keep every MCP tool schema in docs/tools.md consistent with the Go structs in internal/mcpserver/tools.go.

Rationale: docs are the normative schema mirror; drift breaks agent integrations.

## SPX-REPO-002
WHEN a new package is added under internal, the author SHALL add a scoped .spectacle/spec.md bundle or extend an ancestor spec bundle with rules for it.

Rationale: self-hosting requires every subsystem to be under contract.

## SPX-TST-001
WHEN a package is added under internal or cmd, the repository SHALL provide a dedicated `*_test.go` file exercising the package's exported surface.

Rationale: end-to-end tested implementations are the exit criterion of P-0002.

## SPX-SWM-001
The server SHALL confine every agent branch commit to code files by excluding `.spectacle` paths, so spec state reaches main only through the semantic replay.

## SPX-SWM-002
WHEN an item is moved to rejected, the server SHALL write the rejection to the shared `coord.db` event log so sibling agents see it before any merge.

## SPX-SWM-003
IF a scope lease of a live agent overlaps a requested claim, THEN the server SHALL reject the claim with an `l` record naming the holder, the item and the expiry.

## SPX-SWM-004
The server SHALL mint every item ID and rule ID through the shared coordination counters so two parallel worktrees never mint the same ID.

## SPX-SWM-005
WHILE a worktree is open for this session, the server SHALL block the `compact` tool so journal folds cannot corrupt the submit replay.

## SPX-SWM-006 {applies: go:coord.DB.Sweep}
WHEN the stale-agent sweep runs, the server SHALL delete the `agents` rows whose heartbeat is older than `agent_ttl` together with their `leases` rows.

Rationale: the swarm view must show who is actually there; short-lived driver sessions must not accumulate.