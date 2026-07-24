---
schema: v0
---

## T-0027 generalize orchestration wording + fanout guidance (docs, manifest, workflow prompt)
kind: task
state: done
created: 2026-07-24
parent: P-0015

Scope ONLY: README.md (section 'Orchestrated swarm workflow (cheap fresh subagents)'), docs/agent-workflow.md, CONTRIBUTING.md, internal/mcpserver/server.go (instructions const ORCHESTRATION sentence block ONLY), internal/mcpserver/prompts.go (workflow prompt text ONLY) + prompts_test.go if assertions need it. Rules: (1) replace vendor-specific role naming with tiers - 'orchestrator (complex/strong model)' vs 'implementer (simpler/cheaper model)'; keep at most one parenthetical example per doc like '(e.g. a frontier model orchestrating small fast models)' - the repo was built with one specific pairing, that may stay as a historical note in agent-workflow.md, not as the definition. (2) Everywhere state the WHY once, crisply: the brief is written by the complex model, so the simple model never explores; the complex model's context stays free of implementation noise; tokens scale with task count not codebase size. (3) FANOUT: add to README section, agent-workflow.md (own subsection 'Fan-out') and the workflow prompt LOOP block one dense line: orchestrator partitions approved tasks by disjoint scope (leases prove disjointness), spawns one fresh implementer per task in parallel, serializes only shared-file wiring. (4) instructions const: rewrite the ORCHESTRATION block in this spirit (strong->exhaustive briefs->fresh cheap agents->fanout on disjoint leases), same terse style, no vendor names. Verify: go build ./internal/... && go test ./internal/mcpserver/ -race green; git diff of the five files reviewed for leftover vendor-specific definitions (grep -i 'sonnet\|claude' on the touched files: allowed only in the single historical-example parenthetical in agent-workflow.md and in the README driver JSON block which is client config, judge each hit).
