---
schema: v0
---

## P-0018 SDD orchestration v2: research-first, grill, native decisions, bounded loops (blocked state)
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/lifecycle/lifecycle.go, internal/item/item.go, internal/mcpserver/tools.go

Approved plan /root/.claude/plans/du-bist-ein-principal-magical-moonbeam.md. New tools research (condensed problem-space pack, tier 1; research ITEMS stay tier 2), grill (critique pack + grilled: header evidence, fold-proof), decide (native MCP elicitation, git-native async D-items, answer from any session). Anti-ping-pong: rounds counter (reopen + real gate fails), config feedback.max_rounds=3, at limit server sets NEW side state blocked (outside the total order, server-set only) + auto-mints D-item (rescope|reject|override-once) linked via needs:. New item fields rounds/grilled/needs/override, kind decision (D), journal events grill/decide/escalate (+compact retention), replay tolerance. Two-mode /spectacle: bare = state, with requirement = full SDD lifecycle; workflow prompt gains task arg.

## T-0033 docs + two-mode /spectacle wrappers + manifest
kind: task
state: done
created: 2026-07-24
parent: P-0018

Scope ONLY: docs/tools.md, docs/agent-workflow.md, README.md, internal/mcpserver/server.go (instructions const ONLY), new .claude/commands/spectacle.md + .claude/commands/spectacle-state.md. Can run parallel to code tasks (text only; tool sections may cite the approved schemas from the plan). tools.md: sections research/grill/decide with the JSON schemas from plan file /root/.claude/plans/du-bist-ein-principal-magical-moonbeam.md, grammar additions (q-record, b-record for brief findings, blocked in item states, rounds/grilled/needs headers), tool count update. agent-workflow.md: new section 'Decisions, grill & bounded loops': research-before-ask rule, grill checklist flow, decide async workflow (native UI, answer-from-anywhere), rounds/blocked escalation sequence diagram (ascii), the three exits. README: mermaid add blocked side state (server-set, decide exits) + short 'Entry points' subsection: /spectacle bare = state, /spectacle <requirement> = full SDD lifecycle, /spectacle-state alias, /mcp__spectacle__{workflow,next,state}. .claude/commands/spectacle.md: frontmatter description; body: if $ARGUMENTS empty -> call spectacle state tool (MCP if registered, else headless driver per README) and render; else run the full SDD lifecycle for $ARGUMENTS with the 8 steps (research, draft, grill, decide-if-uncertain, approve, fanout cheap implementers, check, archive) and orchestrator/implementer division per docs/agent-workflow.md. spectacle-state.md: state alias. server.go instructions: extend ORCHESTRATION with: research (tool then R-item) BEFORE asking the user; decide for any user decision - never unstructured chat; grill before approving (grilled: evidence); rounds limit -> blocked + D-item, exits rescope|reject|override-once. Verify: make lint-specs green, yaml/markdown eyeball, no Go changes beyond the const.

## D-0001 Merge PR #11 automatically after gates pass?
kind: decision
state: done
created: 2026-07-24

kind: radio
options: merge, hold
choice: merge
