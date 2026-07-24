---
schema: v0
---

## P-0084 surface item references and make grill ask for the weighing that produced the plan
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/tools.go, internal/mcpserver/grill.go

item.Refs landed as storage in T-0109 and nothing can write or read it: draft has no input for it, get never renders it, and grill does not know it exists. A field no tool touches is dead weight, and the two capabilities it was added for are both still missing.

First capability: deliberation chains. A research item can now cite another research item, a proposal can point at the research that produced it, an ADR can name the research that fed the decision — but only once draft accepts refs and get renders them. Validation belongs at the write path, exactly where T-0109 deliberately did NOT put it: UnknownRefs exists as a pure helper precisely so the parser stays permissive (a citation may point at an item archived out of work.md) while the writer refuses a typo. A dangling citation written today is unrecoverable noise tomorrow.

Second capability: grill asks for the weighing. There is currently no step that produces a deliberated plan and no step that notices its absence. research aggregates, draft records, grill critiques — and the reasoning that picked one approach over others survives only if the orchestrator happens to write it into the body.

Rejected, again and deliberately: turning grill into a plan-and-grill step. Its worth is that it works against the author; a step that writes the plan and then critiques it loses that independence entirely. Also rejected: a new record kind for plans. An ADR is already question, options, decision, consequences — an implementation-approach choice is that same shape at a lower altitude, and a near-duplicate kind would split one concept across two vocabularies.

So grill gains a question, not a gate: a proposal that records no weighing — no refs to an ADR or research item, and nothing in its body naming a rejected alternative — gets asked about it, alongside the questions it already raises. A question the orchestrator can answer or dismiss is the cheap version of demanding a plan; a hard gate would block work on the many proposals whose choice really is obvious.

## P-0085 close two follow-ups left open by the knowledge work: duplicated option parser, colliding prose sections
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/decide.go, internal/knowledge/extract.go

Two limitations both implementers flagged rather than hid, each deliberately deferred because the file they needed was held by a sibling task. Both files are free now.

One: the option parser exists twice, byte for byte. T-0110 needed it in internal/knowledge, which must not import internal/mcpserver, so it exported item.ParseOptions with the same three-form fallback logic decide.go's decideOptions has. The duplication was confirmed exact by diffing the two function bodies. Two copies of a parser drift the moment one grows a fourth accepted form, and the failure mode is silent: an ADR whose options parse in one code path and not the other.

Two: every whitelisted prose section maps onto a single entry kind, so two differently-named sections carrying identical text collapse into one entry. The content key is derived from the text alone, which is right for rules — identical text IS the same rule — but wrong for prose, where the section name is part of the identity. An intent paragraph and a design paragraph that happen to read alike are not the same knowledge, and merging them across repositories would silently drop one of the two.

Both are small, and neither is urgent in this repository — it uses only intent sections, and decide.go's copy works. They are worth closing precisely because they are the kind of defect that costs nothing now and is expensive to diagnose later, once a second repository with a design section joins the fleet or someone adds a fourth option form to one copy.

## T-0117 delete the duplicated option parser; give prose entries a section-aware key
kind: task
state: active
created: 2026-07-24
parent: P-0085
targets: internal/mcpserver/decide.go, internal/knowledge/extract.go, internal/knowledge/artifact.go, internal/knowledge/extract_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here. Two independent fixes; do them in either order.

SCOPE (lease exactly these four)
  internal/mcpserver/decide.go            delete decideOptions, call item.ParseOptions
  internal/knowledge/extract.go           section-aware prose key
  internal/knowledge/artifact.go          only if the entry needs a section field
  internal/knowledge/extract_test.go
Do NOT touch internal/mcpserver/tools.go, grill.go or their tests (a sibling task owns all four right now), internal/mcpserver/knowledge.go, internal/item/item.go or options.go (finished), internal/drift, cmd/, README.md or docs/. .spectackle files are server-owned: never edit them by hand.

FIX 1 — one option parser, not two
internal/item.ParseOptions was created because internal/knowledge cannot import internal/mcpserver. Its body is byte-identical to decide.go's decideOptions, confirmed by diffing the two function bodies. Delete decideOptions and its reOutcome regex from decide.go and call item.ParseOptions instead.
Before deleting, diff the two yourself and confirm they are still identical. If they have diverged since — if decide.go's copy grew a form ParseOptions lacks — STOP and report rather than silently dropping a case: that difference would be exactly the bug this fix exists to prevent, already realized.
The three accepted forms and their fallback ORDER are behavior, not implementation detail: repeated `option: <text>` lines first, then the legacy comma-joined `options: a, b, c` line, then the escalation sentence's outcome list. Existing items depend on all three. decide.go's own tests must pass unchanged — if one fails, the parsers were not identical and you have found something worth reporting.

FIX 2 — prose entries keyed by section, not by text alone
Extract maps every whitelisted prose section onto one entry kind, and the content key is derived from the text alone. For rules that is right: identical text IS the same rule. For prose it is wrong — the section name is part of the identity. An intent paragraph and a design paragraph that happen to read alike are not the same knowledge, and merging them across repositories silently drops one.
Make the prose key depend on the section name as well as the text, and carry the section name on the entry so a reader can see which section it came from. Keep rule and ADR keying exactly as it is: rules keyed by normalized text, ADRs keyed by question only (that is what makes two repos answering the same question differently collide into one conflict — do not disturb it).
Watch the round-trip and determinism tests that already exist; a new field must marshal and parse back, and ordering must stay stable.

TESTS (extract_test.go — extend, do not add parallel files)
  1. two sections with different names but identical text produce TWO entries with different keys.
  2. the same section name and text in two sources still produces ONE entry with a pooled count — the dedup that must keep working.
  3. the section name survives marshal and parse.
  4. rule and ADR keys are unchanged by this edit: assert a rule's key and an ADR's key equal what they were before, using literal expected values so the test fails if keying shifts.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/knowledge/... ./internal/mcpserver/... ./internal/item/... -race
  go test ./...
  go vet ./internal/knowledge/... ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint

EXIT CRITERION
Four tests green under -race, decide.go's pre-existing tests green WITHOUT modification, ./... green, vet clean, lint clean, and only one option parser left in the tree — prove it with a grep showing no decideOptions remains.

ROLLBACK
Fix 1 is a deletion plus a call; restoring the function restores the prior state. Fix 2 changes how one entry kind is keyed, which changes artifact bytes for prose entries only — an artifact written before this change still parses, but its prose entries will not dedup against newly written ones. Say so in your report; it is the one non-cosmetic consequence here and it matters for anyone who already exported an artifact.

REPORT BACK
The diff evidence that the two parsers were still identical, the new prose key derivation, each test's real output, the compatibility consequence for already-exported artifacts, and anything you deliberately did NOT do.

## T-0118 wire item.Refs through draft and get; grill asks for the recorded weighing
kind: task
state: active
created: 2026-07-24
parent: P-0084
targets: internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, internal/mcpserver/grill.go, internal/mcpserver/grill_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
item.Refs exists as storage and nothing reads or writes it. Give draft an input for it, get a rendering of it, and grill a question when a proposal records no deliberation.

SCOPE (lease exactly these four)
  internal/mcpserver/tools.go        draftIn input + validation + getItem rendering
  internal/mcpserver/tools_test.go
  internal/mcpserver/grill.go        one new question
  internal/mcpserver/grill_test.go
Do NOT touch internal/mcpserver/decide.go or internal/knowledge (a sibling task owns both right now), internal/item/item.go (Refs and UnknownRefs are finished — consume them, do not change them), internal/drift, cmd/, README.md or docs/. .spectackle files are server-owned: never edit them by hand.

WHAT EXISTS ALREADY (read it, do not reimplement)
  item.Item.Refs []string                                     order-preserving, deduped on write
  item.UnknownRefs(selfID string, refs []string, known map[string]bool) []string
UnknownRefs reports, in input order, every ref that fails item.IDRe, equals selfID, or is missing from known. Parse deliberately does NOT reject unknown refs — a citation may point at an item archived out of work.md, and a parser that refused to load such a file would make a dangling citation unrecoverable. Validation belongs at the write path, which is this task.

PART 1 — draft accepts refs
Add a refs field to draftIn (the input struct in tools.go). Its jsonschema description should say what it is: item IDs this item cites, any kind to any kind, no lifecycle meaning — as opposed to parent, which is structural ownership, and needs, which means blocked-on.
Validate before persisting anything. Build the known-ID set from the items the server can see and call UnknownRefs with the new item's own ID. If it reports anything, reject the WHOLE call with a dense `! ARG` record naming every offending id, and write nothing — no item, no journal event. A partially-written item with a bad ref is worse than a refused call.
Contract: MCP-012 (read it with get id=MCP-012).

PART 2 — get renders refs
getItem in tools.go already renders parent and the ADR fields. Add refs in the same dense style, emitted only when non-empty. Follow the existing lines exactly rather than inventing a format; an agent parses these.

PART 3 — grill asks for the weighing
grillQuestions(it) in grill.go produces the #questions section. Add one question, fired for PROPOSALS only, when the item records no deliberation: no refs pointing at an adr or research item, AND nothing in the body naming a rejected alternative. For the body test, look for the vocabulary this repo's own records already use — a line mentioning a rejected or considered alternative. Keep the heuristic simple and say in a comment that it is a heuristic; a false positive costs one dismissed question, a false negative costs nothing that was not already lost.
This is a QUESTION, not a gate. Do not block move to=approved on it. Many proposals have an obvious single approach, and a hard gate would stall them for ceremony.

TESTS
  tools_test.go:
    1. draft with valid refs persists them; a follow-up get renders them.
    2. draft with an unknown ref is refused with an ! ARG record naming that id, AND nothing was written — assert the item does not exist afterward and no journal event was appended.
    3. draft with a self-reference and with a malformed id are both refused.
    4. draft with no refs behaves exactly as before (regression guard).
  grill_test.go:
    5. a proposal with neither refs nor a rejected-alternative line gets the question.
    6. a proposal that cites an ADR does not.
    7. a proposal whose body names a rejected alternative does not.
    8. a task, not a proposal, never gets the question.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
Then exercise it live: build your binary, serve it on a probed free port, draft an item with a good ref and one with a bad ref, and get the good one. Paste the transcript.

EXIT CRITERION
Eight tests green under -race, the refused-call test proving nothing was persisted, ./... green, vet clean, lint clean, and the live transcript.

ROLLBACK
One input field, one validation branch, one render line, one grill question. Reverting the four files restores the prior behavior; item.Refs stays as unused storage exactly as it is today. No schema, stored format or record migration — items written with refs still parse without this code.

REPORT BACK
The draftIn field and its description, the exact refusal record, the grill heuristic and why you chose it, each test's real output, the live transcript, and anything you deliberately did NOT do.

## P-0087 generate only the two lifecycle commands by default; everything else on explicit request
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/commands.go

The generator emits eight commands today. Six of them — find, get, research, swarm, export, merge — exist for a user exploring or debugging the state machine and its records. Useful, but not part of the lifecycle a repository needs in order to work, and every one of them is a file checked into the consuming repository's harness directory whether that repository wanted it or not.

Split the set by whether the command is load-bearing. The lifecycle entry point and the state snapshot are: without them the workflow has no front door. The exploration commands are not: the same operations remain fully available as MCP tools, which is how an agent reaches them anyway. Generating six files into someone's .claude directory to expose tools their agent already has is a cost with no matching benefit.

So the default set becomes the entry point, the state snapshot, and one more that has to be in the default set for the scheme to work at all: the generator command itself. Without it a user cannot ask for the rest, and a feature reachable only by reading documentation about a tool call is not reachable.

All templates stay in the repository. Nothing is deleted, no capability is removed, and the exploration commands remain one explicit request away. What changes is who decides they exist: the consuming repository, rather than this one on its behalf.

Rejected: dropping the six templates. They are wanted, just not by default, and deleting them would turn an opt-in into a rewrite.

Rejected: a flag on the existing generate operation rather than a command. The point is that a user without an agent session, reading a list of slash commands, can discover that more exist — a flag on a tool call is invisible to exactly the person this is for.

This repository will keep all eight generated, because it is the one place where exploring and debugging the state machine is the daily work. That is not a contradiction of the default; it is the default working as intended, with an explicit request behind it.

## T-0120 default generation is the three lifecycle commands; the rest on explicit request
kind: task
state: active
created: 2026-07-24
parent: P-0087
targets: internal/mcpserver/commands.go, internal/mcpserver/commands_test.go, internal/mcpserver/templates/commands/generate.md.tmpl

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/mcpserver/commands.go and the existing templates before writing anything — the generator became data-driven in the previous round and you are extending that, not redesigning it.

GOAL
Split the generated command set by whether a command is load-bearing for the lifecycle. Default generation emits the entry point, the state snapshot, and the generator command itself. The six exploration commands — find, get, research, swarm, export, merge — are generated only when explicitly requested.

SCOPE (lease exactly these three)
  internal/mcpserver/commands.go
  internal/mcpserver/commands_test.go
  internal/mcpserver/templates/commands/generate.md.tmpl   NEW
Do NOT touch internal/mcpserver/tools.go, grill.go, decide.go, knowledge.go (siblings hold all four right now), internal/knowledge, internal/journal, internal/item, cmd/, README.md or docs/. The regenerated harness surfaces are written BY THE TOOL, never by hand. .spectackle files are server-owned: never edit them by hand.

THE SPLIT
commandSpecs already describes every command. Add a field marking which are in the default set. Default: the workflow entry point, state, and generate. Opt-in: find, get, research, swarm, export, merge.
Why generate must be in the default set: without it a user cannot ask for the others, and a capability reachable only by reading documentation about a tool call is not reachable by the person this is for.
All six templates STAY in the repository. Nothing is deleted; what changes is who decides they exist.

HOW A USER ASKS
commands op=gen keeps its current meaning but emits only the default set. Requesting more is an explicit argument on the same operation — pick a shape that fits the existing input struct (a list of command names, or an all switch, or both) and document it in the tool description. Do NOT add a second tool.
The new generate.md.tmpl is the slash command that drives it: it tells the agent to call commands with the argument that requests the exploration set, and it should list which commands that adds so a reader knows what they are getting.

IDEMPOTENCE AND ALREADY-GENERATED FILES
A repository that already has the six exploration files and then runs a default gen must NOT have them deleted — generation adds and overwrites, it has never removed, and silently deleting a file a user has in git would be the worst possible behavior here. Verify this explicitly and state it in your report.
The AGENTS.md managed block is different: it is rewritten wholesale between its markers. Decide what happens to already-present exploration sections there, implement the non-destructive choice, and say what you chose and why.

THIS REPOSITORY KEEPS ALL EIGHT
After your change, regenerate with the explicit request so this repo's own .claude/commands, .github/prompts and AGENTS.md still carry every command — this is the one place where exploring and debugging the state machine is the daily work. Report the ok gen lines for both the default run and the explicit run, so the difference is visible.

TESTS (commands_test.go — extend, do not add parallel files)
  1. default gen writes exactly the three default commands for claude and copilot, and no others.
  2. explicit request writes all eight.
  3. default gen over a directory that already contains the six exploration files leaves them intact.
  4. the generate template renders, is non-empty, carries the do-not-edit header and names the commands it unlocks.
  5. the existing assertions about file sets are updated in place rather than duplicated.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint

EXIT CRITERION
Five tests green under -race, default gen provably emitting three commands and explicit gen eight, already-generated files provably untouched by a default run, ./... green, vet clean, lint clean, and this repository regenerated with all eight present.

ROLLBACK
One field on commandSpecs, one branch in the writers, one new template. Reverting restores the previous behavior, and because generation never deletes, no consuming repository loses a file either way.

REPORT BACK
The argument shape you chose and why, the AGENTS.md decision and why, both ok gen lists, each test's real output, and anything you deliberately did NOT do.
