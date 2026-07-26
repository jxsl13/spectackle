---
schema: v1
---

## ADR-01KYFYGVSRFX4B9B2YJ44QSBS8 live probe: should the widget cache be bounded
kind: adr
state: done
created: 2026-07-26
decision: yes bounded
status: accepted

kind: radio
option: yes bounded
option: no unbounded
choice: yes bounded

## T-01KYG90P1EE29RSD89Z0PW8CKN node/edge audit: every state hint offers judgment options, no tool result demands mechanical caller work
kind: task
state: approved
created: 2026-07-26
grilled: 2026-07-26 open=0
targets: internal/mcpserver/prompts.go, internal/mcpserver/server.go, internal/mcpserver/tools.go, docs/lifecycle.md

IMPLEMENTER IN OWN WORKTREE. USER PRINCIPLE (2026-07-27, now NODE-EDGE-001): graph NODES are LLM interactions - at each state the LLM decides direction and does judgment work (implement, document, review, test, decide) from the MCP hints; EDGES are fully mechanical MCP steps. No tool result may DEMAND mechanical work of the caller; every state surface must present the judgment OPTIONS, not just one next command.

AUDIT SCOPE, the known suspects enumerated so the implementer verifies rather than hunts:
1. nextAction (prompts.go): currently renders ONE next step per state. The principle says OPTIONS - at minimum the primary action plus the legal alternatives the state machine allows (e.g. done: validate-then-archive OR reopen; draft-grilled-clean: submit OR revise OR reject). Extend to render the primary first plus a compact or-alternatives tail; table test updated to exact lines. Keep it dense - this is the most-read surface (byte-measure before/after, state the delta).
2. The h stale-binary hint (make dev) and workflow template step 9 (Restart): both instruct a MECHANICAL step. The resident self-restarts (committed-only watcher) - the hint is for setups where the dying binary cannot replace itself. Reword both to say the mechanical restart is the SERVERS job where it can (self-restart watcher) and name the one honest exception (no watcher process manager); the LLM decision at that node is only whether to continue on the stale surface. Verify the reworded hint still satisfies its staleEligible test.
3. IMPLEMENTER PROTOCOL (promptNext tail): verify each numbered line is a judgment or a single tool call that triggers mechanics - no raw git, no manual file plumbing (expected clean since ROLE-BOUNDARY-001; assert it in a test that greps the rendered protocol for the gitInstructionViolations patterns, reusing the bench detector).
4. docs/lifecycle.md: one paragraph stating the principle with NODE-EDGE-001 cited - nodes are judgment, edges are mechanics, hints enumerate options.
NON-NEGOTIABLE, tested: nextAction per-state table updated with alternatives (exact strings); rendered promptNext protocol passes gitInstructionViolations with zero hits; stale-hint wording change keeps its existing gate tests green; byte deltas stated.
VERIFY: go build/test -race/vet/gofmt; lint; check ok; paste the new nextAction table output for done and draft states.
SCOPE: the four files + tests. No lifecycle.go, no gitflow.go.
ROLLBACK: revert.
REPORT: byte deltas, the audit verdict per suspect, anything found beyond the enumerated four.
