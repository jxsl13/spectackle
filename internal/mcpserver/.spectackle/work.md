---
schema: v0
---

## P-0071 records are written in American English: mandate it in the manifest and the generated harness surfaces
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/server.go, internal/mcpserver/templates/commands/workflow.md.tmpl

Requirement: the server must prescribe American English as the language of every record it stores. Records are read by agents and humans across sessions and harnesses; a corpus mixing the operator's spoken language with English makes find/research results unsearchable by the terms the code and rules actually use, and rule sentences are already English by EARS construction, so mixed-language item bodies split the same concept across two vocabularies.

The existing RECORDS paragraph already governs record STYLE (compacted substance, no verbatim quotes). Language is the same axis and belongs in the same paragraph rather than a new one — one place agents read, one place to keep true.

American, not just English: spelling variants (behavior/behaviour, initialize/initialise) fragment FTS matches in the same way a language mix does. The repo's existing rule and doc corpus is already American; this makes the de-facto standard explicit rather than introducing a new one.

Delivery surfaces must both be covered, as the compacted-records rule showed: the instructions manifest reaches MCP clients through the initialize handshake, while agents driving the lifecycle through the generated slash commands never see it. Manifest plus workflow template plus regenerated harness files, and a contract so the manifest cannot silently lose the paragraph.

## T-0101 RECORDS paragraph mandates American English; regenerate the harness surfaces
kind: task
state: active
created: 2026-07-24
parent: P-0071
targets: internal/mcpserver/server.go, internal/mcpserver/templates/commands/workflow.md.tmpl, internal/mcpserver/tools_test.go, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
The server must prescribe American English as the language of every stored record. Today the RECORDS paragraph in the instructions manifest governs record style (compacted substance, no verbatim quotes) but says nothing about language, so an operator working in another language produces item bodies that no later find or research query reaches with the English terms the code and rules use.

SCOPE (disjoint, lease exactly these four)
  internal/mcpserver/server.go                              extend the existing RECORDS paragraph
  internal/mcpserver/templates/commands/workflow.md.tmpl    extend the existing Records paragraph
  internal/mcpserver/tools_test.go                          extend the existing manifest test
  docs/agent-workflow.md                                    extend the existing records prose section
Then regenerate the three harness surfaces from the template (see below) — AGENTS.md, .claude/commands/*.md, .github/prompts/*.prompt.md are GENERATED, never hand-edited.
Do NOT touch internal/resolve (a sibling task owns that whole directory), internal/mcpserver/decide.go, docs/roadmap.md or docs/design-wasm-parsers.md. .spectackle files are server-owned: never edit them by hand.

CONTRACT TO SATISFY
MCP-007 (added under this proposal, read it with get id=MCP-007): the manifest SHALL carry a RECORDS paragraph requiring American English, compacted substance, and no verbatim quotes in every item body.

EDITS
1. server.go: the manifest's RECORDS paragraph currently mandates compacted substance and forbids verbatim quotes. Add the language mandate to that SAME paragraph — do not open a new one; agents read the manifest top to bottom and a second paragraph on the same axis is a second place to keep true. State American English specifically, and say why in the same breath the paragraph already justifies its other rules: spelling variants (behavior/behaviour, initialize/initialise) fragment full-text matches exactly like a language mix does. Keep the manifest's terse register — it is a dense instruction sheet, not prose.
2. workflow.md.tmpl: the Records paragraph near the end mirrors the manifest for agents that drive the lifecycle through generated slash commands and never see the initialize handshake. Add the same mandate there, in that paragraph.
3. Regenerate the harness surfaces. Do NOT hand-edit them. From your worktree root:
     go build -o /tmp/spx-gen ./cmd/spectackle
     /tmp/spx-gen serve -root . -http 127.0.0.1:7399 &
     then call the commands tool with op=gen and harness=[claude,copilot,codex] over that endpoint
   If you cannot drive the HTTP endpoint, use a one-shot stdio JSON-RPC exchange instead (initialize, notifications/initialized, tools/call). Either way the tool must do the writing. Expected output: five ok gen lines (two claude, two copilot, one codex). Kill the server afterwards.
4. tools_test.go: there is already a test asserting the manifest's BROWNFIELD IMPORT and RECORDS paragraphs are present. Extend it — do not add a parallel test — so it also fails if the American-English mandate disappears from the manifest. Assert on a substring that is meaningful rather than incidental punctuation.
5. docs/agent-workflow.md: it carries a prose section on records for human readers. Extend it with the same rule, matching the surrounding voice.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
  confirm the mandate is present in all three generated surfaces AND in the two sources (five files) by reading them

EXIT CRITERION
The extended test fails when the mandate is removed from server.go (prove this: delete the sentence, run the test, see it fail, restore it) and passes with it present; ./... green; lint clean; the mandate readable in AGENTS.md, .claude/commands/spectackle.md and .github/prompts/spectackle.prompt.md, all three produced by the generator rather than by hand.

ROLLBACK
Prose in two sources plus regenerated output plus one test assertion. Reverting is a git checkout of the four files followed by one more generator run; no schema, stored format, record or anchor changes.

REPORT BACK
The exact text you added to each of the five files, the generator's five ok gen lines, the output of every verify command, and the observed failure message from the deliberate removal step.
