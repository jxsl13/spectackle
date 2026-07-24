---
schema: v0
---

## P-0014 MCP prompts: /spectacle workflow entry points (workflow + next)
kind: proposal
state: approved
created: 2026-07-24
targets: internal/mcpserver/server.go

Expose the working mode as MCP prompts so clients surface them as slash commands (/mcp__spectacle__workflow, /mcp__spectacle__next). Both are DYNAMIC: workflow embeds live state (root rules via the cascade, active items, agents/leases from coord.db) plus the compact loop; next takes an optional item argument, defaults to the first approved task, and returns its full brief plus the implementer protocol (lease claim -> move active -> implement -> move done -> release). Server-side registration wired by the orchestrator.

## T-0025 implement prompts.go: workflow + next (no server.go wiring)
kind: task
state: done
created: 2026-07-24
parent: P-0014

Scope ONLY: new internal/mcpserver/prompts.go + prompts_test.go, docs/tools.md (new short section 11 'Prompts'). Verify SDK API via go doc github.com/modelcontextprotocol/go-sdk/mcp Server.AddPrompt and GetPromptResult/PromptMessage BEFORE coding - no guessed fields. Implement method (s *Server) registerPrompts() called by nobody yet (orchestrator wires): AddPrompt workflow (no args): handler locks s.mu, builds text: line 1 'spectacle workflow - state below is live'; section AGENTS/LEASES from s.cd.Agents()+s.cd.Leases(s.agentTTL()) as ag/l lines; section ACTIVE ITEMS from item.LoadAll(s.ws) as i lines (state != draft first); section LOOP: 6 dense lines mirroring the instructions const steps; return single user-role PromptMessage. AddPrompt next (optional string arg item): if empty pick first item with state approved (kind task preferred, else any); if none -> message 'ok nothing approved - draft or approve first'; else text = full brief: item.Record + parent + targets + body verbatim + the 5-step implementer protocol with the concrete item ID and its dir as suggested lease path. Tests: connectRoot pattern from tools_test.go, session.GetPrompt (verify client API via go doc) for both prompts: workflow contains 'LOOP' and an ag line; next with no approved -> 'nothing approved'; after draft+move approved task -> next contains the task ID and 'lease'. go build ./internal/... && go test ./internal/mcpserver/ -race green (registerPrompts must be called in the test setup directly since New does not wire it yet - call s.registerPrompts() after New in the test helper OR export nothing and test via s.MCP() after manual call).
