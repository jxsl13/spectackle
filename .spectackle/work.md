---
schema: v1
---

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

## T-01KYFXBYMZFH7BC4Z1JQT8FV73 ambiguity findings computed from post-deletion signals, closed by decide round-trip or recorded waiver
kind: task
state: draft
created: 2026-07-26
parent: P-01KYESGDWFFMH80ENHNFXMVZE8
targets: internal/mcpserver/grill.go, internal/mcpserver/decide.go, internal/mcpserver/tools.go, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read the parent P-01KYESGDWFFMH first - it carries the user-approved design (ADR-01KYES0TR: waivable with reason plus tripwire) and the rejected alternatives; do not re-litigate.

WHY. A vague requirement today grills clean if it has targets and anchors; the guess gets implemented and the correction rounds dominate cost. The server must compute AMBIGUITY and force an interactive user round-trip or an accountable waiver - never a silent guess.

WHAT TO BUILD, all in grillComputed (the a-e sibling classes are the pattern):
1. THREE AMBIGUITY CLASSES, post-deletion signals only (word-presence checks are padding-gameable and rejected): amb-thin - kind task|proposal in draft whose body is under 400 bytes after whitespace flattening (CALIBRATE against this repo first: render the byte-size distribution of the last 20 archived task bodies in the report and adjust so none of them would have fired); amb-novel - every target path lands in a package failing COVERED (reuse uncoveredPackages, cite T-01KYD87ZN) AND find scope=history on the title returns nothing (zero prior art anywhere); amb-incoherent - three or more target paths spanning three or more top-level dirs with no graph edge between any pair (use the loaded graph; two targets never fire it).
2. ASK CLOSURE, mechanical: an open ambiguity finding closes when the item Refs a decide-minted adr item whose state is done or archived (the research-consumption verbatim-token precedent); while that ADR is open the finding renders as one line g amb-... awaiting ADR-<short>. WAIVE closure: the existing per-finding waiver machinery, unchanged.
3. ASK NUDGE: decideAsk's need-decision fallback line gains the suffix - surface this through your harness interactive ask; a parked record is not an ask (ASK-SURFACE-001). Draft-pack guidance one sentence: ask at draft time so user latency overlaps other work.

NON-NEGOTIABLE, each tested: a 200-byte task draft receives amb-thin; the same body padded to 401 bytes of real sentences does not regress the OTHER classes (no word-presence anywhere - grep the diff for banned patterns like strings.Contains(body, and justify each hit); an item with Refs to a done ADR renders the finding closed, to an open ADR renders awaiting, without Refs stays open; waived amb finding passes the verdict gate exactly like any waived key; calibration table pasted in the report; existing grill tests untouched and green.
VERIFY: go build/test -race/vet/gofmt clean; lint 0 findings; check ok; live: draft a deliberately thin task on this repo, paste its grill render showing amb-thin, close it via a real decide ask+answer round-trip, paste the closed render, then reject the throwaway item with a note.
SCOPE: grillComputed + decideAsk fallback line + draft pack sentence + docs/agent-workflow.md elicitation paragraph. No state.go, no validate.go, no new tools, no config keys.
ROLLBACK: revert commit.
REPORT: calibration distribution, banned-pattern grep, live round-trip transcript, each test result.
