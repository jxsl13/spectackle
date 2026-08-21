---
schema: v1
---

## B-01KYTK0BGRF7X9PEY3QKWQ7TQA a rule added outside any record lifecycle has no edge to commit it, which invites bypassing the branch and PR flow
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/tools.go

OBSERVED on myself, which is the only reason it is worth filing: I added RECMERGE-004 with rule op=add while no record was active. The server wrote spec.md and made its own spectackle-paren-rule edge commit, and I then pushed that commit straight to main - bypassing the one-task one-branch one-PR flow this repository otherwise holds to. Nothing else rode along and main is clean, but the path was wrong and I reached for it because the alternative was unclear.

THE GAP. Every other write in this system is carried by a lifecycle edge: a move commits, pushes, opens or merges a PR, and the gates run. A standing rule is explicitly meant to be added the MOMENT a learning emerges - the workflow says so - which is frequently not while a record happens to be active. So the one write the process most wants to be immediate is the one write with no edge to carry it. The natural next step for an agent in that position is a manual git command, which is exactly the behavior the flow exists to prevent.

WHAT IS NOT THE FIX: telling agents to always have an active record first. That inverts the instruction to cast learnings immediately, and it would make a rule addition cost a full lifecycle - which is how learnings end up in agent-private notes instead, the failure the instruction is written against.

DIRECTION, and this needs a decision rather than a patch. Either a records-only write gets its own lightweight edge - commit, push, PR, gates - so the flow covers it, or rule op=add explicitly states that its write lands on the current branch and will be carried by the next edge, so the agent knows to leave it alone rather than improvise. The second is nearly free and may be sufficient; the first is more correct if rules should never sit uncommitted. Measure how long a pending rule typically waits for the next edge before choosing - if it is minutes, the cheap option wins.

VERIFY. Add a rule with no active record and confirm the surface tells the caller what happens to the write and what NOT to do about it. If the edge option is chosen instead, confirm the gates run on it and that a rule cannot reach main without them.

## T-01M0K3TT29EWST67ZG0TVGERYS surface and validate records-only context classifications
kind: task
state: active
created: 2026-08-21
parent: P-01M0K371V9EDV9Z3RTJEM4JBG7
refs: P-01M0K371V9EDV9Z3RTJEM4JBG7
grilled: 2026-08-21 open=1
targets: docs/tools.md, internal/spec/author.go, internal/spec/author_test.go, internal/journal/journal.go, internal/replay/replay.go, internal/mcpserver/context.go, internal/mcpserver/context_test.go, internal/mcpserver/state.go, internal/mcpserver/tools.go, internal/mcpserver/tools_test.go

Scope
Compute context classification at the MCP boundary, expose it in state/check, and add a supported metadata mutation. Do not alter coverage-gate defaults or rule cascade semantics.

Files
- internal/mcpserver/context.go and internal/mcpserver/context_test.go
- internal/mcpserver/state.go and internal/mcpserver/state_test.go
- internal/mcpserver/tools.go and internal/mcpserver/tools_test.go
- the existing MCP registration file
- docs/tools.md when required by schema mirroring

Required APIs and behavior
1. Build one context status per workspace.ContextDirs entry from material presence, spec rules, and nonterminal items in that context. Class values are physical, historical-only, and records-only-live. The root context is always physical.
2. Add one #contexts state section with one compact cx record per non-root records-only context, including dir, class, declared mode, live-rule count, open-item count, and unresolved-binding count. Keep state read-only.
3. Add typed check errors context-records-only-live, context-logical-open-work, and context-logical-unbound-rule. Add warning context-logical-stale. Historical-only without logical mode passes.
4. Add context tool with op=set, dir, and mode. Mode accepts logical or empty or physical to clear. It rewrites only spec frontmatter through an atomic server-owned path, preserves all body bytes, validates the prospective classification before writing, and refuses logical mode with open work or unbound rules.
5. Register and document the schema consistently.
6. Tests cover physical, historical-only, undeclared live, valid logical, missing applies, stale applies, open work, stale declaration, read-only state, and mutation byte preservation. Existing coverage tests remain unchanged.

Verification
go test ./internal/mcpserver
go test ./...
go vet ./...
