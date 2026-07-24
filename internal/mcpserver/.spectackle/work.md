---
schema: v0
---

## P-0074 manifest tells tool users where to report spectackle bugs: issue yes, fix PR no
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/server.go

An agent driving this server is in the best position to notice a defect in the server itself — it observes tool behavior against its own expectations on every call — and today nothing tells it what to do with that observation. The two bad outcomes both happen in practice: the agent works around the defect silently, so nobody upstream ever learns of it, or it goes and patches the server it is currently using, which is a scope violation dressed as helpfulness.

Policy to state: report, do not fix. An issue carrying the agent's analysis (reproduction, observed vs expected, the isolated cause when known) is high-value because that analysis is the expensive part. An unsolicited fix PR is not: the reporter lacks the design context, the change lands in a codebase whose maintainers did not ask for it, and reviewing it costs more than the analysis saved.

The destination must be derived, not hardcoded. runtime/debug.ReadBuildInfo() yields the main module path, which for any Go module hosted on a forge is exactly the https URL of its repository. A literal URL in a string rots the moment the module moves; a derived one cannot. Fall back silently to the compile-time module path when build info is unavailable (test binaries, some build modes).

Surfaces: the instructions manifest and the workflow template, the same pair every other manifest policy uses, since agents driving the lifecycle through generated slash commands never see the initialize handshake.

Explicitly out of scope: any tool that opens issues. The agent has its own forge access; a server-side issue-filing tool would duplicate it, require credentials the server has no business holding, and grow the tool surface for something the agent can already do.

## T-0104 manifest paragraph: report defects as issues at the derived module URL, never as fix PRs
kind: task
state: active
created: 2026-07-24
parent: P-0074
targets: internal/mcpserver/server.go, internal/mcpserver/templates/commands/workflow.md.tmpl, internal/mcpserver/tools_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Tell the agent using this server what to do when it notices a defect IN the server: open an issue carrying its analysis at the module's repository, and do not send a fix PR. Today nothing states this, and both failure modes occur — silent workarounds that never reach the maintainers, and unsolicited patches to the server the agent is currently running against.

SCOPE (disjoint, lease exactly these three)
  internal/mcpserver/server.go                            new manifest paragraph + URL derivation
  internal/mcpserver/templates/commands/workflow.md.tmpl  mirrored short form
  internal/mcpserver/tools_test.go                        assertions
Then regenerate the three harness surfaces with the commands tool (step 4). AGENTS.md, .claude/commands/*.md and .github/prompts/*.prompt.md are GENERATED — never hand-edit them.
Do NOT touch cmd/spectackle or README.md (a sibling task owns both right now), internal/resolve (another sibling owns it), internal/mcpclient, or docs/. .spectackle files are server-owned: never edit them by hand.

CONTRACT TO SATISFY
MCP-008 (added under this proposal; read it with get id=MCP-008).

URL DERIVATION — the part that must not be hardcoded
Use runtime/debug.ReadBuildInfo(): bi.Main.Path is the main module path (github.com/jxsl13/spectackle for this repo), and for any module hosted on a forge that path prefixed with https:// is the repository URL. Derive it once, lazily or at manifest-composition time.
Fallbacks, in order: bi.Main.Path when non-empty; otherwise a compile-time constant holding the same module path. ReadBuildInfo returns ok=false in some build modes and Main.Path can be empty in test binaries, so the fallback is load-bearing rather than defensive decoration — write a test that exercises the derivation helper with an empty path and asserts it still yields the right URL. Do NOT add a dependency and do NOT parse go.mod at runtime.

PARAGRAPH CONTENT (server.go manifest)
Add it as a NEW paragraph, not folded into RECORDS — this is about the agent's relationship to the server, not about how records are written, and the existing paragraphs are each one axis. Match the manifest's terse register and its ALLCAPS-label style. It must say: where to report (the derived URL), what a useful report contains (reproduction, observed versus expected, the isolated cause when known — the analysis is the expensive part and the reason a report is worth filing), and that a fix PR is not wanted. Give the reason for that last point rather than only the rule: the reporter lacks the design context, and reviewing an unsolicited patch costs more than the analysis saved.

WORKFLOW TEMPLATE
One or two sentences carrying the same policy, in the template's voice, for agents that drive the lifecycle through generated slash commands and never see the initialize handshake.

REGENERATE (step 4) — the tool does the writing, not you
  go build -o /tmp/spx-gen-v ./cmd/spectackle
  /tmp/spx-gen-v serve -root . -http 127.0.0.1:7401 &
then call the commands tool with op=gen and harness=[claude,copilot,codex] against that endpoint. A working Streamable-HTTP client is at /tmp/claude-0/-home-user-spectackle/d0b8e016-f097-5792-857b-fd9ea4a8a781/scratchpad/mcp_http.py (base URL as argv[1], one JSON call per stdin line). Expected: five ok gen lines. Kill the server afterwards.

TESTS (tools_test.go)
  1. Extend the existing manifest test so it fails if the paragraph disappears. Assert on the policy substring AND on the derived https:// URL appearing in the manifest — the second assertion is what catches a regression to a hardcoded or missing URL.
  2. New unit test for the derivation helper: empty module path yields the fallback URL; a non-empty path yields https:// + that path. Table-driven.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint

EXIT CRITERION
Both tests green; the manifest test proven to fail when the paragraph is removed (delete it, run, observe, restore); ./... green; vet clean; lint clean; the policy readable in all three generated surfaces, produced by the generator.

ROLLBACK
One manifest paragraph, one template paragraph, one helper, two test additions, regenerated output. git checkout of the three sources plus one generator run restores the prior state. No schema, stored format, record, anchor or dependency change.

REPORT BACK
The exact paragraph text, the helper's implementation, the five ok gen lines, every verify command's real output, and the failure message from the deliberate-removal step.
