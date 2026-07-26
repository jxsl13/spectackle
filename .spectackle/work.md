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

## T-01KYFXDC6KFNCB66W8JVHTZZNK lens-labeled verdicts under the single-reviewer sequential default, with per-item panel opt-in capped by config
kind: task
state: done
created: 2026-07-26
parent: P-01KYESGDWFFMH80ENHNFXMVZE8
grilled: 2026-07-26 open=0
targets: internal/mcpserver/grill.go, internal/journal/journal.go, internal/mcpserver/server.go, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Parent P-01KYESGDWFFMH carries the settled review-mode economics (REVIEW-MODE-001, one reviewer walks lenses sequentially; panel sizing by computed risk REJECTED with three kills) - implement, do not re-argue.

WHAT TO BUILD:
1. LENS LABELS: grill op=verdict gains optional lenses (comma list, e.g. correctness,security,refute); journal Event gains Ln []string (json ln, 10-20B); the verdict render and reviewState surface them (verdict ... lenses=a,b,c). Findings text convention documented: prefix per-lens findings with [lens]. No validation of lens NAMES (vocabulary is the reviewers), but an empty-string lens is an ARG error.
2. SEQUENTIAL-DEFAULT INSTRUCTION, in the orchestration guide topic (server.go guideTopics) and the grill tool description: one reviewer, explicit perspective reset between lenses, per-lens quota - findings or an explicit [lens] none line, never silence - refutation lens LAST. Keep the addition under 350 bytes across both surfaces combined (measure, state the bytes).
3. PANEL OPT-IN, per item only: grill op=verdict panel=<n> records intent for MULTI-agent review; legal ONLY when a risk signal is live on the item - an open irreversibility finding (class c), a prior verdict-contradicts-evidence refusal, or override-once spent (Ov). Otherwise ARG-refused naming the missing signal. Config swarm.panel_max (default 3) CAPS n; config can never raise a panel that was not item-justified. Each panelist verdict is a separate EvReview from its own identity (existing machinery unchanged); the gate still needs only one passing verdict - panel is evidence breadth, not consensus voting (state this in the tool description).
NON-NEGOTIABLE, tested: lenses stored and rendered round-trip; empty lens string refused; panel without risk signal refused naming the signal; panel over cap refused; verdict without lenses still valid (back-compat); guide-text byte cap measured in a test with the base named.
VERIFY: build/test -race/vet/gofmt; lint; check ok; live: one real verdict on a throwaway draft with lenses=minimality,tokens,refute pasted.
SCOPE: named files + tests. No decide.go, no validate.go changes.
ROLLBACK: revert; Ln is additive, old journals parse.
REPORT: byte measurements, refusal transcripts, live render.

## T-01KYFXDCHHFV3SQDA5CEHAA4AQ validation require is risk-gated from the landed diff: file count and dangerous-path membership, never declared targets
kind: task
state: approved
created: 2026-07-26
parent: P-01KYESGDWFFMH80ENHNFXMVZE8
grilled: 2026-07-26 open=1
targets: internal/mcpserver/validate.go, internal/workspace/workspace.go, docs/lifecycle.md

IMPLEMENTER IN OWN WORKTREE. Parent P-01KYESGDWFFMH: break-even for the validation gate is a ~30-50 percent catch rate, so feedback.validate stays warn GLOBALLY and require flips per item from RISK computed off the LANDED diff - never declared targets (T-0135 landed 15 files against 4 declared; gaming kill). This repo keeps blanket require in its own config - the risk gate is for everyone else; nothing here may weaken an explicit require.

WHAT TO BUILD:
1. RISK INPUTS, computed in validateGateGap from itemDiff (already attributed): landedFiles = count of distinct files in the diff; dangerous = any file under a dangerous path. Dangerous list: config feedback.dangerous_paths ([]string, glob per SkipDir conventions); DEFAULT when key absent: internal/lifecycle/**, internal/journal/**, internal/workspace/**, .github/** on this repos vocabulary is WRONG for other repos - so the shipped default is empty plus a scaffold comment documenting the knob; risk from paths fires only when the user configured it. File-count threshold: feedback.risk_files (int, default 8, scaffold-documented).
2. GATE SEMANTICS: validate=require behaves as today. validate=warn (or absent): the archive gate REQUIRES a passing verdict IFF landedFiles >= risk_files OR dangerous matched; otherwise warns as today. The refusal names the tripped input: validation required: landed 12 files >= 8. An explicit require is never downgraded.
3. Scaffold config.yaml documents both knobs (workspace_test key list grows by two).
NON-NEGOTIABLE, tested: warn-mode item with a 9-file landed diff refuses archive without verdict naming the count; same item at 7 files archives with warning; dangerous-path hit at 1 file refuses; require-mode unchanged (existing tests); knobs parse, absent means default 8 and empty list; scaffold lists both keys.
VERIFY: build/test -race/vet/gofmt; lint; check ok; live proof deferred to the bench A/B (measuring catch rates needs the outcome fixture, noted as the follow-up in the parent).
SCOPE: validateGateGap + config + scaffold + docs/lifecycle.md gate paragraph. No grill.go.
ROLLBACK: revert; knobs additive.
REPORT: refusal line verbatim, each test, knob defaults rationale.

## T-01KYFXEPW3FD7RYDR1Q0S1R4FF waiver-rate tripwire: a computed non-vetoing line in state and packs when waivers dominate recent verdicts
kind: task
state: approved
created: 2026-07-26
parent: P-01KYESGDWFFMH80ENHNFXMVZE8
grilled: 2026-07-26 open=0
targets: internal/mcpserver/state.go, internal/mcpserver/grill.go, internal/mcpserver/validate.go, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Parent P-01KYESGDWFFMH / ADR-01KYES0TR: ambiguity (and every) finding is waivable with a reason, and the counterweight is a TRIPWIRE - computed, visible, never vetoing. A gate that vetoes on waiver rate would just teach padding the findings; visibility teaches judgment.

WHAT TO BUILD:
1. COMPUTATION, one pure function (own file waiverrate.go, mcpserver package): over the TRAILING 20 verdict events (EvReview + EvValidate, both kinds pooled, newest first across all context journals), rate = waived keys / (waived keys + addressed-open keys); events whose renders had zero open findings are excluded from the denominator entirely (a clean streak must not dilute the signal). Threshold 0.5. Below: silence. At or above: one line - w waiver-rate 62% over last 13 verdicts (waived=8 addressed=5) - counts shown so the reader can judge the base size; fewer than 5 qualifying verdicts: silence (sample too small, state in a comment).
2. SURFACES: state (#health section) and the grill/validate PACK renders (one line, after the verdict line). Never in check (CI string-matches ok - the coverage-gate lesson, T-01KYD87ZN).
3. DOCS: three sentences in agent-workflow.md - what it measures, why it never vetoes, what a high rate suggests (briefs too thin, classes too eager, or reviewers rubber-stamping - all three are human calls).
NON-NEGOTIABLE, tested: synthetic journals - 10 verdicts with 6 waived/4 addressed renders the line with those counts; 4 qualifying verdicts renders nothing; zero-open verdicts excluded from the denominator (test pins it); rate below threshold silent; check output byte-identical with a tripping journal present (golden).
VERIFY: build/test -race/vet/gofmt; lint; check ok; live: state on this repo pasted (whatever it shows - this session waived heavily, the line may well fire; that is signal, not embarrassment).
SCOPE: new waiverrate.go + one render line in three files + docs. No gate logic anywhere.
ROLLBACK: revert.
REPORT: the live state line or its absence with the computed rate, each test.

## T-01KYFXEQ71F8X9C2028Q3B4WBX verdict events survive compaction and findings render only pre-archive: the token diet closes the loop
kind: task
state: approved
created: 2026-07-26
parent: P-01KYESGDWFFMH80ENHNFXMVZE8
grilled: 2026-07-26 open=0
targets: internal/mcpserver/tools.go, internal/mcpserver/grill.go, docs/lifecycle.md

IMPLEMENTER IN OWN WORKTREE. Parent P-01KYESGDWFFMH / ADR-01KYES0TT: verdict events survive compaction. VERIFIED GROUND: they do NOT today - compact()s keep-list (tools.go ~2416) retains reject/archive/compact/escalate/decide and DROPS EvReview and EvValidate, so a compaction erases the identity-bound evidence the gates rest on; a re-validation after compact would find no verdict and refuse an already-validated archive... except archived items are past the gate - the REAL loss is the audit trail and reviewState/lastGateResult on still-live items whose verdicts predate the fold.

WHAT TO BUILD:
1. KEEP-LIST: add journal.EvReview and journal.EvValidate to the retained cases with a comment citing ADR-01KYES0TT and the loss shape above.
2. PRUNE THE DEAD WEIGHT: retained verdicts of items already archived or rejected at compact time MAY drop their Keys and Wv slices (the addressal detail) while keeping ev/id/ag/hash/pass/ln - the verdict identity survives forever, the per-key forensics only while the item lives. Implement as part of the same fold pass; comment the byte rationale.
3. FINDINGS PRE-ARCHIVE ONLY: the grill and validate PACK renders skip the #computed findings section for items in archived state (tombstone readers want the verdict trail, not a re-critique; the classes recompute against a tree that has moved on and mislead). One sentence in each tool description; render a single line computed: suppressed (archived) instead of the section.
4. DOCS: lifecycle.md compaction paragraph gains the retention vocabulary (verdicts retained, addressal detail pruned post-terminal).
NON-NEGOTIABLE, tested: a compact fold over a journal with live-item verdicts keeps them byte-complete; over archived-item verdicts keeps ev/hash/pass and drops Keys/Wv; reviewState still resolves a retained verdict after compact (integration: grill render -> verdict -> compact -> reviewState finds it); archived-item pack renders the suppressed line and no v/g classes; live-item pack unchanged (golden).
VERIFY: build/test -race/vet/gofmt; lint; check ok; live: run compact dry-run on this repo, paste the c candidates, do NOT apply (the resident owns its own compaction cadence).
SCOPE: compact keep-list + the two pack renders + docs. No journal.go schema changes (Ln lands in the lens task - coordinate through the lease if concurrent).
ROLLBACK: revert; retention is strictly additive to todays behavior.
REPORT: before/after fold of a synthetic journal, each test, the dry-run paste.

## ADR-01KYFYGVSRFX4B9B2YJ44QSBS8 live probe: should the widget cache be bounded
kind: adr
state: done
created: 2026-07-26
decision: yes bounded
status: accepted

kind: radio
option: yes bounded
option: no unbounded
choice: yes bounded
