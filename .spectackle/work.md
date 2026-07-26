---
schema: v1
---

## P-01KYD87FJREJ5SD0G2RDCMZ32Y turn review from assertion into evidence: run the gate that exists, then make grill and validation compute what they cannot fake
kind: proposal
state: approved
created: 2026-07-25
refs: R-0007, P-01KYD47GZ7FAMAGM4NEF0BQS8T
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/evidence

Supersedes the draft it cites in refs, which carried two errors its own validation round caught; corrected here, everything else re-recorded intact.

R-0007 completed: six lenses planned, four reported before the session limit killed the rest, 40 mechanisms proposed, 34 naming a real failure from this repository's history. The second pass verified its predecessors against the live server and the code, and it overturned the first synthesis on its top-ranked detector.

FINDINGS VERIFIED INDEPENDENTLY BY THE ORCHESTRATOR, not taken on report:
- The submit gate executed ZERO commands in this repository's entire history: config.yaml carried no verify key and no goal field was ever added to any item, so runGate built its command list from two empty sources and returned success on all seven submits. Fixed first (verify commands armed, proven to bite in a scratch workspace before enabling; measured cost about fifteen seconds per run).
- The mechanism the first synthesis ranked second - a monoculture scan over the target package's test files - would NOT have caught B-0004: the literal main lives in internal/wt/wt.go:298 inside InitTestRepo, production code in a different package; naive literal frequency is noise (op 102 hits, id 84, main not in the top 50). Four lenses converged on a mechanism that does not work as specified. Convergence across lenses is not verification.
- grill is ceremony in practice: twelve grill events, every one 0-91s after its item's create event, three within the same second, zero bodies ever revised in response. P-0088 was grilled 3m25s BEFORE its child briefs existed.
- check cannot report a contract gap here: the root bundle is unscoped with sixteen rules, so ForPath never returns empty and the coverage branch is unreachable. ELEVEN of twenty-four packages under internal/ carry no bundle (thirteen do) while SPX-REPO-002 mandates one - the prior draft said twelve, recounted by the independence validator at eleven. Six of the ten dogfooded defects landed in uncovered packages.

RANKED REMAINDER, by verified failures per unit of cost (1 = the armed gate, done):
2. Server-computed environment differential at grill: live values of a fixed axis list beside what the item's tests construct. Four of ten defects in one section, roughly thirty lines.
3. grill stamps a verdict bound to what it read; move gates on the verdict, not a non-empty date. P-0060's adjudicated principle applied to the reviewer.
4. Package-local contract coverage with the applies-binding mitigation (a lazily written root sentence with no applies binding silences nothing). Eleven violations visible today.
5. Blast radius and irreversibility. CORRECTED SCOPE from the anti-ceremony validation: at grill time, on declared targets, this is a TRIPWIRE only - T-0137 gamed the word-check with a well-formed paragraph and a heading check is gameable the same way, and T-0135's 4-declared/15-landed divergence is invisible pre-implementation by construction. Declared-vs-landed belongs to the post-implementation validation phase's diff computation, where it is computable exactly.
6. Declared-but-unconsumed sweep (B-0009's title is the finding).
7. Caller-divergence sweep, minority argument shapes (B-0003 was one against twenty).
8. Server-executed mutant-kill gate at submit - strongest evidence generator after the first, deferred on its measured eighty-second tax until the anti-ceremony lens re-runs against real measured costs of 2-7.
9. Independent-oracle recall for recognizers, as a ratchet - would have caught R-0005 wholesale; the only mechanism with a maintenance tail; deferred with 8.

TO DELETE RATHER THAN ADD: grill's word-presence questions and the brief substring heuristics - including the substring half of the deliberation check itself (strings.Contains on the word rejected, found by this set's own validation round). They cannot fail for a determined author, they train bodies to grow padding, and they occupy the slot where a computed check belongs.

CHILD TASKS: the grill-verdict and validation-phase tasks live under the review-and-validation proposal (verdict machinery, identity binding); under THIS proposal: package-local coverage and the evidence sweeps. Scope is disjoint by file; rollback for each is the removal of one section or one predicate.

## P-01KYD87FX0F6YRX49R3A8TB6E4 backpropagation: every loop result flows back into the workspace, and the server names the next step so no step can be silently skipped
kind: proposal
state: approved
created: 2026-07-25
refs: R-0007, P-01KYD6VP6VE2Z8A517AT3RP39T
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, internal/mcpserver/tools.go, docs/agent-workflow.md

Supersedes the draft it cites in refs: that draft's scope note carried a dangling task ID the independence validator proved resolves to not-found, and its note-requirement remedy contradicted its own no-written-signals standard; both corrected here, everything else re-recorded intact.

PROBLEM. The loop's forward path is well defined: research, draft, grill, approve, implement, check, archive. The backward path - how results change the workspace so the next iteration is smarter - exists only as convention. Three symptoms, each verified in this repository: (1) the server's own backprop concept covers exactly one flow, code-to-spec drift (check fix=true drafts one proposal per drifted rule, tools.go:1733) - research results, implementation reports and rejections have no defined return path; (2) the workflow template's final step says archive and commit but not what must be captured; the archive note is the training signal - it becomes the journal tombstone and the FTS body future sessions search - yet nothing says so and an empty note passes; (3) the template omits the post-merge restart entirely: CONTRIBUTING.md mandates make dev because the resident server IS the product under change, and the machine-facing instructions never mention it - real stale-binary confusion resulted this session.

WHY IT MATTERS FOR TOKEN COST. Knowledge that does not land in one of the three durable stores (spec.md rules, journal tombstones with substantive notes, knowledge artifacts) is re-derived by a later session at full exploration price. The backward path is the token-saving mechanism, not an overhead on it.

ENFORCEMENT LAYERING, stated once for all note requirements in this set: prose reminders in templates are guidance; length floors are tripwires against accidental emptiness, gameable by padding and known to be; SUBSTANCE is enforced only by hard gates bound to computed facts - the research-consumption gate (child task here) and the validation verdict gating archive for task and bug kinds (the validation-phase task under the review-and-validation proposal). A note requirement that is only prose is listed as guidance, never claimed as a control.

DELTA. Two child tasks:
1. Define the loop's backward edges in every machine-facing surface (server instructions, workflow template, next-step prompt) so each state names its one next action and each completed item states where its learning landed. Bounded: hints are one line, computed from actual state.
2. Enforce the research return path at the one gate that can see it: archiving an R-item requires either a consumer (a live or archived item or rule citing it) or an explicit no-action note. One conditional at one call site.

EXPLICITLY REJECTED: a generic workflow engine; any always-on background process; LLM-written self-assessments as evidence.

EXIT CRITERION. A fresh orchestrator session driven only by the server's own prompts performs research capture, archive notes, and post-merge restart without any of them being in its own system prompt - measured by driving the loop once headlessly and checking the three stores gained the expected records.

ROLLBACK. Each surface change is a template/instruction edit; the R-item gate is one conditional. Reverting the commit restores the prior loop; no data format changes.

SCOPE DISJOINTNESS. Task 1 touches server.go/prompts.go/templates/docs. Task 2 touches the move path in tools.go, which the grill-verdict task under the review-and-validation proposal (title: grill computes its critique and stamps a verdict) restructures first - task 2 declares NEEDS on that task BY TITLE and runs after it merges. No task ID is cited here by prefix guess; the prior draft's lesson is recorded: reference sibling work by exact title phrase or full minted ID, never by a predicted prefix.

## T-01KYD87ZN7EJ49CMSEQE9XGGWS package-local contract coverage: silent by default with visibility in state, counted by check only under coverage_gate
kind: task
state: approved
created: 2026-07-25
parent: P-01KYD87FJREJ5SD0G2RDCMZ32Y
refs: R-0007, T-01KYD72GQ6E2ZV0HX8S443NPY6
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/mcpserver/state.go, internal/workspace/workspace.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs, whose central output design was proven impossible against the real code by this set's anti-ceremony validation; the corrected design is below and the impossibility is part of your ground truth.

NEEDS: the grill-verdict task (title: grill computes its critique and stamps a verdict) must be MERGED first - it restructures tools.go's move path and this task edits tools.go's check path; the lease serializes regardless. ALSO DISCLOSED: T-01KYD2XQG6E38APSR3EY4GY137 (rule op=edit recomposition) is an open draft targeting internal/spec/author.go AND internal/mcpserver/tools.go - if it becomes active before you, coordinate through the lease and rebase on whichever merges first.

WHY. check cannot report a contract gap in this repository: the root bundle is unscoped and carries 16 rules, so spec.Cascade.ForPath never returns empty and the coverage branch (tools.go:1695-1716, fires on len(ForPath(rel))==0) is structurally unreachable. ELEVEN of twenty-four packages under internal/ carry no bundle (thirteen do - counted by this set's independence validator; the prior draft said twelve) while SPX-REPO-002 mandates one, and check answers ok. Six of the ten dogfooded defects landed in uncovered packages.

THE OUTPUT-CHANNEL CONSTRAINT, verified against the real code - this is why the prior draft was impossible: check() has exactly ONE path that renders ok: if len(lines) == 0 return text("ok") (tools.go:1679-1680); any non-empty lines returns budget.Render(kept, cur) verbatim with NO trailing ok (budget.go:68-76 is a plain newline join; text() at tools.go:147-148 is a pass-through). The CI self-hosting gate does FULL-STRING equality: result != "ok" exits 1 (ci.yml:71-76). Therefore ANY unconditional visible output from check - twenty lines or one summary line - turns this repository's own CI red on merge. Visibility-without-gating cannot live in check's output channel. Do not rediscover this; design around it as specified.

WHAT TO BUILD
1. COVERED(pkg): a source dir under internal/ or cmd/ is covered iff (a) a non-root bundle exists at it or an ancestor below the root, or (b) at least one root-bundle rule binds a node inside it via applies (resolve applies targets to paths through the anchors table; a rule with empty applies never covers anything outside its own dir). This is the mitigation: a lazily written root-level EARS sentence with no applies binding silences nothing. Cost: O(rules x anchor rows), both already in memory - no new I/O; state this holds in a code comment.
2. DEFAULT VISIBILITY lives in state, not check: state's #rules section already renders one line per dir (ok dir <d> rules=<n>); append the token uncovered to dirs failing COVERED. state is not string-matched by CI - VERIFY includes proving that (read ci.yml; only check's output is compared to ok). Zero new lines, one token appended to existing lines - no output growth beyond 10 bytes per uncovered dir.
3. GATING: workspace config key coverage_gate: package (FeedbackCfg sibling or top-level key - pick, justify) makes check emit g nocontract <dir> lines (sorted, capped 20 + "+<n> more" tail) that COUNT as findings - CI red until backfilled, by explicit opt-in only. Default absent: check emits NOTHING for coverage - identical output to today, byte for byte, proven by a test.
4. This repository does NOT set the key in this task. The report lists the eleven dirs as the backfill worklist.

NON-NEGOTIABLE PROPERTIES, each with a test
- Byte-identity default: on a workspace with uncovered dirs and no key, check output is byte-identical to pre-change (golden test).
- state marks exactly the uncovered dirs; adding one applies-bound root rule into pkg X removes exactly X's token.
- With coverage_gate: package, check emits the capped records and does NOT end ok; without, it does.
- Cap holds: 40 uncovered dirs -> 20 lines + tail.
- Unknown-key tolerance: a workspace that sets the key loads on a server built without this change (YAML ignores unknown keys - verify, state in report).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok AND is byte-identical to a pre-change run on the same tree (paste both, diff empty)
  spectackle call -root <worktree-root> state '{}' - paste the #rules section showing the eleven uncovered tokens.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the golden byte-identity test and the gated-mode test from the diff alone; verdict recorded in the archive note.

SCOPE: coverageGaps and the check wiring in tools.go, the state.go rules-section token, the config key in workspace.go, tests. Do not touch grill.go, the spec package, or the anchors format.
ROLLBACK: revert the commit; the config key is additive and ignored by older servers.
REPORT BACK: the COVERED implementation, both pasted check runs with empty diff, the state section, the eleven-dir worklist, each test's result, anything deliberately not done.

## T-01KYD88KV5EX2SBYE81TKYHDH9 the backward path in every machine-facing surface: state-computed next steps, archive notes as the training signal, post-merge restart in the loop
kind: task
state: approved
created: 2026-07-25
parent: P-01KYD87FX0F6YRX49R3A8TB6E4
refs: R-0007, T-01KYD72HB0FHX9G80DQGS9YBB1
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs. Its validation round found: a wrong rule pointer stated twice (SPX-MCP-006 governs the research tool; the rule that pins TOKEN ECONOMY is MCP-006), a substance test left as prose, and two unbounded/unmeasured caps. All corrected below; everything else re-recorded intact.

WHY. The loop's backward path is convention, not definition. An LLM driving the loop from the server's own surfaces is never told: that the archive note becomes the journal tombstone and the FTS body every later session searches (the note IS the training signal); that research results must land as rules, ADRs, tasks or an explicit no-action note; that after every merge the resident server must be rebuilt (CONTRIBUTING.md mandates make dev; the machine-facing surfaces never mention it - real stale-binary confusion this session); or what the single next action is for the state an item is in. Each omission costs a later session full re-derivation price.

ENFORCEMENT LAYERING (from the parent proposal, restated because it bounds this task): everything this task ships is GUIDANCE surfaces plus computed next-step hints. The hard gates for note substance live elsewhere: the research-consumption gate (sibling task) and the validation verdict gating archive for task/bug kinds (the validation-phase task under the review-and-validation proposal). This task must not claim its template prose enforces anything.

VERIFIED GROUND (do not re-derive)
- server.go instruction manifest: ORCHESTRATION and TOKEN ECONOMY paragraphs (~line 44). The rule pinning TOKEN ECONOMY is MCP-006 (internal/mcpserver/.spectackle/spec.md:67, applies go:mcpserver.Server.registerTools; its rationale at spec.md:73 says so). SPX-MCP-006 is a DIFFERENT rule about the research tool - do not touch it, do not cite it.
- Manifest-content test precedent to MIRROR (T-0098 pattern, cited in spec.md): multi-substring assertions on the manifest.
- templates/commands/: exactly 8 tmpl files; workflow.md.tmpl has exactly 8 steps, step 7 Check, step 8 Archive+Commit/PR, no make dev anywhere. commands op=gen regenerates command files from templates.
- prompts.go: promptNext (line 158) picks an actionable item and renders its brief; skips blocked and open-needs items. lifecycleLines at :68.
- check fix=true already drafts one backprop proposal per drifted rule (tools.go:1733) - the code-to-spec direction exists; reference it, never duplicate it.

WHAT TO BUILD
1. server.go: one BACKPROP paragraph, <= 700 bytes, stating: the three durable stores (spec.md rules, journal tombstone notes, knowledge artifacts); every completed or rejected item leaves its learning in one of them; the archive/reject note is searched by future sessions - write substance; after every merge, make dev (the server is the product under change). Add an EARS rule via rule op=add binding go:mcpserver.Server.registerTools, mirroring MCP-006's pattern exactly.
2. workflow.md.tmpl: step 7 gains independent re-verification (a fresh-context verifier re-runs the VERIFY block from the diff alone - the implementer's transcript is never the evidence); step 8 gains the note guidance (what changed, what was measured, what was deliberately not done, where the learning landed); new step 9: make dev after merge, one sentence why; new step 10: research capture - every R-item ends consumed or explicitly closed, citing that the server enforces this at the archive gate. TOTAL template growth <= 40 lines, MEASURED: a test counts the template's lines with a hardcoded ceiling and a comment naming the measured pre-change base (the validation round found the prior draft asserted this cap but never measured it).
3. promptNext: the rendered output's FIRST line is the one computed next action per state: draft ungrilled -> grill; draft grilled-with-open-gaps -> close gaps, re-grill; submitted -> approve or reject; approved -> work op=start; active -> implement, then work op=submit; done -> check, then move to=archived with note; blocked -> decide op=answer on the linked ADR. Table test over fixture items in each state asserting the exact first line.
4. docs/agent-workflow.md: BACKWARD PATH section, <= 30 lines (the prior draft left this cap unstated), human-facing mirror of 1-3, plus the independent-verification sentence in the orchestrator role.

NON-NEGOTIABLE PROPERTIES, each with a test
- Manifest SUBSTANCE, computed (mirror T-0098): assertions that the manifest contains BACKPROP, all three store names, the phrase make dev, and the training-signal sentence fragment - multi-substring, not presence-of-paragraph.
- Manifest size ceiling: current measured size + 800 bytes, hardcoded with the base named in a comment.
- Template line ceiling test as in (2).
- promptNext table test as in (3).
- commands op=gen on a temp workspace regenerates files containing steps 9 and 10.
- No lifecycle behavior changes: existing suite passes untouched.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  spectackle call -root <worktree-root> commands '{"op":"gen"}' succeeds; paste the regenerated steps 9-10.
CROSS-VERIFICATION (orchestrator, after done): independent verifier regenerates the commands file, diffs against the report's claim, and re-runs the manifest substance + size tests from the diff alone; verdict recorded in the archive note.

SCOPE: the four named files, generated command files, the one new EARS rule, tests. Do not touch tools.go, lifecycle.go, grill.go. No new tools, no config keys.
ROLLBACK: revert the commit; run commands op=gen once to restore prior command files. The added rule retires via rule op=retire. No stored state.
REPORT BACK: manifest base and final sizes, template base and final line counts, the regenerated steps verbatim, each test's result, anything deliberately not done.

## P-01KYD8HSZ0ERTBFBBEVQD68M4R the commit log is the decision log: every state-machine edge commits with a structured message, and no merge may flatten the trail
kind: proposal
state: approved
created: 2026-07-25
refs: R-0007, P-01KYD7QT8YE6PAT515BGPQ5VM4
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/wt/wt.go, internal/workspace/workspace.go, CONTRIBUTING.md, docs/agent-workflow.md

REQUIREMENT. Every phase and step taken through the MCP must be followable by a human in git alone: each state-machine edge produces its own commit whose structured message describes the decision (what moved, from where to where, by whom, and the reasoning note), and no merge policy may flatten that trail - pull requests are merged with merge commits, never squashed, because a squash collapses N decisions into one blob and destroys exactly what the per-edge commits create.

WHY THIS EXTENDS RATHER THAN DUPLICATES THE JOURNAL. The journal is the machine's replay log; git is where humans and reviewers already look. Today the two are reconciled only when an orchestrator remembers to commit, in batches with hand-written messages - this session's own history shows multi-edge batch commits whose messages summarize rather than enumerate. Deriving the commit from the journal event at write time makes the two logs agree by construction, with zero agent effort - the driving LLM issues no git command and needs no git knowledge (prior user authorization, extended here from phase checkpoints to every edge).

VERIFIED GROUND. One choke point covers the whole tool surface: gate[T] (tools.go:175-185) wraps every handler under s.mu with preCall/postCall; only the rule tool inlines the identical pattern (tools.go:200-210). Every state edge already appends a journal event with agent identity (journal.Append stamps ag). wt.CommitCode (wt.go:111) is the committing precedent. P-0009 (archived) established one move call per forward jump - the commit granularity inherits it: one call, one edge traversal, one commit, even when the jump skips states.

DESIGN DECISIONS, alternatives rejected:
1. Commit composed FROM the journal event(s) the call appended, in the same gate, after the handler succeeds. Rejected: a background committer (races the next call, decouples decision from commit); a git hook (cannot know the decision, only the diff).
2. Full record IDs in commit subjects and trailers, never display-short forms - a short prefix is the shortest unambiguous prefix AT THAT INSTANT (T-0136) and a commit message is immutable, so a later mint could make an archived subject ambiguous. Machine-readable trailers (Spectackle-Ev, Spectackle-Item, Spectackle-From/To, Spectackle-Agent, Spectackle-Eid) make the log queryable with plain git log --grep and interpret-trailers.
3. The server commits ONLY paths under .spectackle trees it wrote in that call, via explicit-pathspec commit semantics so a user's concurrently staged work is never swept in. Code commits remain the implementer's and the submit path's business.
4. Never push. Remotes stay with the orchestrator and CI.
5. Config git_commits: edges|off, DEFAULT edges - supersedes the phases|off knob drafted in the validation-phase task, whose git section this proposal's engine task subsumes.
6. Merge policy is enforced where it lives: the GitHub repository setting (allow merge commits, disallow squash) is the hard control and is the user's one manual step, documented; CONTRIBUTING.md, the agent workflow docs and the machine-facing instructions all switch from squash to merge so no agent re-introduces flattening.

CROSS-CLONE CAUTION, stated because it is the known sharp edge: two server processes on one checkout can race the git index. The commit step must take the same cross-process serialization the whole-file-rewrite task establishes through coord.db, and must retry on index.lock. The lost-update work (B-01KYD57F line) is the prerequisite ordering.

EXIT CRITERION. Drive one full item lifecycle headlessly (draft, grill, verdict, approve, start, submit, validate, archive); git log --oneline then shows one commit per edge in order, each message carrying the full item ID and the decision note; git log --grep on the item ID returns the complete decision history and nothing else. A squash of that branch is impossible through the documented merge path.

TOKEN COST. Zero marginal agent tokens (commits are side effects); the git overhead is one commit per tool call that wrote state - milliseconds, no network.

ROLLBACK. git_commits: off disarms the engine without a rebuild; reverting the commits removes it. Edge commits already made are inert history. The merge-policy docs revert with the commit; the repo setting reverts by hand.

CHILD TASKS: (1) the edge-commit engine in gate; (2) the merge-policy switch across CONTRIBUTING, docs and instructions. The validation-phase task sheds its narrower git section to the engine at its next redraft.

## P-01KYD9466KEPWBV2RBK7EQM202 review and validation are recorded independent verdicts: grill reviews the draft with feedback and a research path, a validation phase judges the implementation, both bound to reviewer identity
kind: proposal
state: approved
created: 2026-07-25
refs: R-0007, P-01KYD7QT8YE6PAT515BGPQ5VM4, R-0005
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/journal/journal.go, internal/lifecycle/lifecycle.go, docs/lifecycle.md

Supersedes the draft in refs: its central claim - identity binding as a computed invariant - was proven overclaimed by this set's own validation round against the real code, and the corrected design plus the honest limit are below. Everything else re-recorded intact.

PROBLEM. Two review moments exist in the loop and neither is a review. Before implementation, grill renders a pack and stamps a date - the stamp records that rendering happened, not that anyone read the pack or fed anything back; twelve grills in this repository's history changed zero bodies. After implementation there is no phase at all: the submit gate runs commands, check scans the workspace, done rolls into archived on the orchestrator's say-so - nobody is charged with judging correctness, test honesty, benchmark honesty, or completeness, and whatever the orchestrator notices reaches the implementer as chat, not as a recorded finding the next round is held to.

DESIGN DECISIONS, with rejected alternatives:
1. NO NEW LIFECYCLE STATES. done -> active is the single sanctioned backward hop (docs/lifecycle.md:142) with a reopen counter, feedback.max_rounds and escalation (SPX-SWM-007). Validation gates done -> archived and reopens through the existing hop. Rejected: a new validating state - touches every state-order comparison and replay path for a distinction the reopen counter already expresses.
2. VERDICTS ARE RECORDED EVENTS BOUND TO CONTENT AND IDENTITY. A verdict is a journal event carrying the reviewing agent's identity and the hash (or git SHA range) of what was reviewed; the server refuses a verdict whose identity matches the item's author or implementer, and refuses a verdict from an EPHEMERAL identity - server.go:173 reads SPECTACKLE_AGENT once per process and falls back to coord.GenName(), a random name, so without the ephemeral refusal a per-call client with the env unset would pass the author-check by pure chance (found by this set's validation). A shared resident connection carries one identity for all callers (B-0002 lineage), so verdicts are per-call stdio operations with a deliberately set agent name - the docs say exactly that.
THE HONEST LIMIT, stated here because the earlier draft claimed too much: what the server computes is deliberate-identity divergence, not independence. It defends against FORGETTING to use a separate reviewer - the failure mode R-0007 documented as fakeable-by-forgetting - not against a driver minting a second name purely to clear the gate while sharing every blind spot. That residual is accepted, stated in code comments and docs, and mitigated only by process guidance (fresh context or different model for reviewers), which the server cannot verify. Rejected alternative: claiming the check IS independence - that is how overclaims calcify.
3. THE SERVER RENDERS AND RECORDS; AGENTS JUDGE. The server computes the packs and refusals, records verdicts, gates moves. The judgment - reading, deciding, writing findings - is agent work in a fresh subagent. Findings are floored (non-empty on fail, warn under 80 chars) and capped (2000 bytes stored): the one artifact whose value depends on the reviewer thinking must be neither empty nor unbounded. Rejected: the server scoring quality itself - that is how the word-presence checks happened.
4. GRILL MAY DEMAND RESEARCH, NOT PERFORM IT. When the pack's computed classes surface unknown territory (uncovered target paths, zero history/rejection hits), grill emits a research-needed record counting as an open gap until the item cites an R-item that names the flagged path or term VERBATIM (token match, not semantic scoring - and not any-R-item, which this set's validation showed was gameable). Grounding: R-0005 is the defect class - thirty of thirty-two language recognizers shipped with gaps because novel territory was entered without a study; a demanded R-item is the study. Rejected: grill spawning research - the server cannot run agents and a blocking tool violates SPX-MCP-001.

WHEN EACH STEP HAPPENS: research (on grill demand) and grill-with-verdict between draft and approved - move to=approved gates on a clean independent review verdict. Implementation active -> done as today. Validation between done and archived - move to=archived gates on a clean independent validation verdict; findings reopen done -> active with the findings as the implementer's next brief, counting a round; max_rounds escalates to blocked as today. Nothing else moves.

CHILD TASKS: one supersedes the earlier grill-verdict draft (computed classes, verdict event, identity+ephemeral refusals, research-demand); one builds the validation phase (pack, verdict, gate, reopen feedback, note auto-fill from the verdict). Git checkpoint commits are OWNED by the commit-log-is-the-decision-log proposal, not here.

TOKEN BOUNDS. Verdict events are one journal line; findings capped at 2000 bytes; packs budget-truncated; independence checks O(item's journal events), already loaded. Mutant-kill and oracle-ratchet stay out of scope until the anti-ceremony lens re-runs against measured costs.

EXIT CRITERION. On this repository: a draft receives an independent review verdict from a second, deliberately named agent identity and cannot reach approved before; a done item with a planted vacuous test receives a validation finding, reopens with the finding as its brief, and cannot reach archived until re-validation is clean; an ephemeral-identity verdict is refused with the exact record.

ROLLBACK. Both gates sit behind config strictness mirroring feedback.grill (require|warn); removing the key returns to warn, reverting the commits returns to today. Verdict events in journals are inert history for a reverted server.

## T-01KYDA92TGF3FT994TEE6EDVN6 one task, one branch, one pull request: every finished task opens its own PR immediately, never batched
kind: task
state: approved
created: 2026-07-25
parent: P-01KYD8HSZ0ERTBFBBEVQD68M4R
refs: R-0007, T-01KYDA17KBFV3SBCEV79KXW7Z3
grilled: 2026-07-25
targets: CONTRIBUTING.md, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Documentation-and-policy task, no Go code. Supersedes the rejected draft in refs: the user set the merge decision back to a human step - PRs open immediately but are NEVER auto-merged - and added the always-pushed rule; both are folded in, everything else re-recorded intact.

NEEDS: the merge-policy task (title: merge policy: never squash) must be MERGED first - both rewrite the same CONTRIBUTING section and this task extends the policy that one establishes. Do not run concurrently.

WHY. The decision-trail principle has three granularities and the third was convention, not policy: edge commits make every state transition visible inside a task; never-squash merges keep those commits readable on main; and PR-per-task makes the task itself the unit of review and merge. Batching several finished tasks into one pull request re-creates at the PR level exactly what squashing creates at the commit level - N decisions flattened into one reviewable blob, reviewers approving work they cannot attribute, and a revert that cannot take one task back without taking its neighbors. This session's own history is the proving case: multiple finished work items accumulated on one branch and shipped in combined PRs, so no single task can be reverted, bisected, or re-reviewed in isolation.

THE POLICY, exact: when a task reaches done, its branch is pushed and a pull request is opened IMMEDIATELY - before the next task starts, not at end of session. One task per PR; the PR title carries the full task ID. NO AUTO-MERGE: merging is a human decision, a judgment step per the steps-are-judgments principle - the agent drives CI to green on the open PR (diagnose and push fixes on red) but never merges and never arms auto-merge; when it merges by hand on instruction, the method is merge commit, never squash (sibling policy). ALWAYS PUSHED, ALWAYS COVERED: at no point may changes exist that are not pushed and not covered by an open PR or draft PR - work in progress lives on its pushed branch under a DRAFT pull request from the first commit, flipped to ready when the task is done. An unpushed local change is invisible to every other agent and survives no container; a pushed branch without a PR is work nobody can find from the review surface. Both states are forbidden at any time, not just at task end. Work that never was a task (a typo fix in passing) rides with the task that touched it or becomes a task if it stands alone - nothing merges outside a PR either way, which CONTRIBUTING already mandates. The swarm's worktree branches (spectackle/<item-id>) already give every task its own branch; this policy makes the PR boundary follow the branch boundary instead of collapsing branches into a shared integration branch first.

WHAT TO BUILD
1. CONTRIBUTING.md: the pull-request section gains the one-task-one-PR rule with the granularity rationale above compressed to a short paragraph, including the explicit anti-pattern (a session-accumulation branch carrying several finished tasks) and the exception handling (follow-up commits to an OPEN unmerged PR for the same task are fine; a merged PR is finished and follow-ups are a new task, which the section already states for branches).
2. docs/agent-workflow.md: the orchestrator's git duties gain: open a draft PR with the first pushed commit of a task, flip it to ready and stop pushing when the task is checked and done, drive CI to green, leave the merge to the user; never hold finished work hostage to unfinished siblings, never leave any change unpushed or uncovered. One sentence on the payoff: revert, bisect and review all regain task granularity.
3. Consistency sweep: no remaining sentence in either file implies batching finished work or opening PRs at session end (grep-based check as in the sibling task, state the method).

NON-NEGOTIABLE PROPERTIES
- The CONTRIBUTING section, read cold, answers: when is the PR opened (draft at first push, ready at done), what does it contain (one task), what is in the title (the full task ID), who merges (the user, by hand, merge-commit method), and what may never exist (unpushed changes; pushed branches without a PR). A verifier answering from the text alone is the test, mirror the sibling task's approach.
- No contradiction with the never-squash section or the auto-merge prerequisites it documents - the sections must read as one coherent policy.
- go build ./... and the full suite still pass (docs must not break anything embedded).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  grep -n batch CONTRIBUTING.md docs/agent-workflow.md - paste; hits must be the anti-pattern text itself.
CROSS-VERIFICATION (orchestrator, after done): independent verifier reads both sections cold and answers the four policy questions from the text alone; verdict recorded in the archive note.

SCOPE: the two named markdown files. No Go code, no templates (the backward-path task owns them - if its workflow step 8 wording conflicts once both land, the LATER merger reconciles and says so in its report).
ROLLBACK: revert the commit.
REPORT BACK: the final CONTRIBUTING paragraph verbatim, the grep output, any reconciliation with the sibling tasks' text, anything deliberately not done.

## P-01KYESGDWFFMH80ENHNFXMVZE8 requirement elicitation, lens-labeled verdicts under a single-reviewer default, and the panel amendments: closing the vague-requirement gap at minimum token cost (re-record)
kind: proposal
state: approved
created: 2026-07-26
refs: P-01KYD9466KEPWBV2RBK7EQM202, R-0007
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/mcpserver/decide.go, internal/journal/journal.go, docs/agent-workflow.md

RE-RECORD of P-01KYERRMHSFYH8CY01A624ZZBV, whose body was lost when an adversarial-review subagent reverted live work.md writes with git checkout (see the stub note there and the isolation bug this incident minted). Design unchanged, user-approved via ADR-01KYES0TJ with knobs set by ADR-01KYES0TM (risk-gated require, always-require on this repo), ADR-01KYES0TR (ambiguity findings waivable with reason plus tripwire), ADR-01KYES0TT (verdict events survive compaction).

AMENDS the review chain in refs; changes nothing that chain already decided; adds the elicitation loop and the review-mode economics a three-lens design review (minimality, token accounting, anti-ceremony) plus a user directive settled.

STATE MACHINE VERDICT, recorded first because the requirement demanded it: 6+2 is optimal, no change. Every pairwise merge fails on a live code path: approved-into-active deletes queue semantics that must survive cache deletion and breaks workAborts return path; active-into-done deletes the only rounds-incrementing edge, collapsing max_rounds/escalation; done-into-archived runs spec intent merge before validation. rejected stays virtual (journal snapshot), blocked stays materialized (escalation must be an event a config change cannot retroactively undo). The chains no-new-states decision is upheld with one rationale restatement: the cost of a new state is NOT replay (final-state-wins, vocabulary-agnostic) but the order-comparison sites, abort/demotion rules, and frozen journal vocabulary. A verdict bound to a body hash expresses staleness; a state cannot - that is why every phase the user named is a verdict-gated edge, not a state.

REVIEW MODE - THE USER DIRECTIVE, now the default by design: multi-perspective review is ONE reviewer walking the configured lenses SEQUENTIALLY in one context, not one agent per lens. The dominant panel cost is N-times context ingestion; judgment tokens are marginal. Lens discipline moves into the review instruction: explicit perspective resets between lenses, a per-lens output quota (findings or an explicit empty declaration - never silent), refutation lens last. One verdict event carries per-lens findings under lens labels. What is lost is inter-reviewer independence, which the chains honest-limit paragraph already concedes the server cannot verify; reviewer-not-author holds unchanged. True multi-agent panels are per-item opt-in only, reserved for risk signals: irreversibility findings, verdict-contradicts-evidence, override-once in play. REJECTED with three kills: risk-COMPUTED panel sizing (automation dictating how much judgment is required - inverts steps-are-judgments; gameable via declared targets, T-0135 landed 15 files against 4 declared; same-driver lenses converge, R-0007 proved convergence is not verification) and global per-gate lens config (silently converts per-risky-item cost into per-every-item cost; config may CAP N, never raise it).

ELICITATION - the genuinely new piece: when the requirement is vague, the server must drive an interactive user round-trip, not a guess. Ambiguity findings computed ONLY from post-deletion signals (thin-brief length, zero-history/uncovered-path novelty, target-set incoherence - never word-presence checks, which the chain deletes as padding-gameable). Under per-finding addressal an ambiguity finding closes one of two ways: ASK - the item cites a decide-minted ADR and that ADR is done (mechanical closure, verbatim-token precedent from research-demand), rendered as awaiting ADR-x while open; or WAIVE with a recorded reason plus the computed non-vetoing waive-rate tripwire line in state and pack. The need-decision fallback line additionally hints that the orchestrator surface the question through its harnesss interactive ask - a parked record is not an ask (user directive). Ask-at-draft-time guidance overlaps ask latency with other work; a fully-gated queue stalling while the user is away is correct behavior, accepted and stated.

TOKEN BOUNDS, measured not asserted: chain at one lens takes per-item cost from ~3-4K to ~9-14K tokens; validation-gate break-even is a ~30-50 percent defect-catch rate, so feedback.validate stays warn globally and require is risk-gated from the LANDED diff (file count, dangerous-path membership - never declared targets); this dogfooding repo sets require on itself. Elicitation findings cost ~0.5KB each and pay at a 2-5 percent catch rate. Lens label field costs 10-20 bytes. One pack render per bodyHash shared across all lens verdicts.

CHILD TASKS (ordered): key-truncation exemption first (finding KEYS enumerate compactly and are exempt from budget truncation, BODIES truncate - blocks the chains addressal semantics); ambiguity findings + ask-addressal; lens label + per-item panel config with the single-reviewer sequential default in guide texts; validation-gate risk inputs from landed diff; waiver-rate tripwire; token diet (findings rendered only pre-archive, verdict-event compaction survival per ADR-01KYES0TT documented and tested).

EXIT CRITERION on this repository: a thin-briefed draft receives an ambiguity finding; a decide round-trip answered from a second session closes it mechanically; a reviewer walking three lenses in one context produces one verdict event whose per-lens findings all require addressal; the gate refuses a verdict on a stale body hash; bench curves show the per-item review cost within the stated bounds.
