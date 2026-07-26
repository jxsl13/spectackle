---
schema: v1
---

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

## B-01KYFPNCK2E2QVSAF3QDS1W11S http CLI path silently drops SPECTACKLE_AGENT for identity-bound verdicts
kind: bug
state: draft
created: 2026-07-26
targets: cmd/spectackle/main.go, internal/mcpserver/validate.go

REPRO: SPECTACKLE_AGENT=cross-val-87zn spectackle call -http <addr> validate op=verdict records the verdict as the SERVER agent (dogfeed-orchestrator), while the same env on a -root direct call stamps correctly. OBSERVED during T-01KYD87ZN cross-verification: the independence evidence exists only in the findings text, not in the recorded identity. EXPECTED: identity-bound events (validate/grill verdicts) driven over -http must carry the caller identity - either forward SPECTACKLE_AGENT per call (header or input field) or refuse verdict ops over -http when the caller identity equals the item author, forcing the caller to pick a channel that records truthfully. Silent identity substitution is the defect: it fabricates independence. DESIGN NOTE: per-call agent field must not allow spoofing an arbitrary identity onto OTHER event kinds; scope it to verdict-bearing ops or sign it with the ephemeral-agent machinery (ag-[0-9a-f]{4}).

## T-01KYFPNCXEERJ90CB7STB4ZGVZ dup detector must ignore hunk context-only functions; unify short8/shortHash as its proof
kind: task
state: draft
created: 2026-07-26
targets: internal/mcpserver/validate.go, internal/evidence/dup.go, internal/mcpserver/tools.go

OBSERVED (T-01KYD87ZN validation): v dup go:mcpserver.short8 ~= go:mcpserver.shortHash 100% fired although BOTH functions predate the diff - short8 sat in hunk CONTEXT lines adjacent to inserted code, so the hunk-scoped extraction treated it as touched. RULE: a dup finding must implicate only functions with at least one ADDED line in the attributed diff; context-line-only functions are preexisting code the task never wrote. IMPLEMENTATION: when mapping diffHunks to functions in validateDups, track added-line ranges (+ lines only, not context) and intersect with function spans before lookup in the dup index. PROOF: unify short8 (tools.go, 8 chars) and shortHash (validate.go, 12 chars) into one parameterized helper as the cleanup this false positive pointed at, and add a regression test where a diff INSERTS code adjacent to one twin of a preexisting dup pair and validateDups stays silent, plus one where the diff ADDS a twin and it fires. Byte-budget neutral: no output-format change.
