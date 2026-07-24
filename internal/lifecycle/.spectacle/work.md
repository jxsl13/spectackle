---
schema: v0
---

## T-0014 implement forward-skip transitions + Mermaid automaton in README
kind: task
state: done
created: 2026-07-24
parent: P-0009

Scope ONLY: internal/lifecycle/lifecycle.go + lifecycle_test.go, README.md (new '## Lifecycle - the finite automaton' section with mermaid stateDiagram-v2 after 'The loop'), docs/lifecycle.md (state machine paragraphs), docs/tools.md (move tool prose), internal/mcpserver/server.go (instructions const: rewrite step (4)/(7) sentences to teach forward skips, one edit). Replace edge-map transitions with total order draft<submitted<approved<active<done<archived: forward jumps legal, rejected from any non-terminal with note, rejected revocable to draft/submitted/approved/active, archived terminal. Keep Allowed(from) API (compute list). active->archived and beyond-done skips run the archive effects exactly once. Update tests: old forbidden-skip assertions flip to allowed; add skip-path e2e draft->active->archived and reject-from-done.
