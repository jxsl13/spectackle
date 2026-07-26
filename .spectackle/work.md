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

## T-01KYFSQQ7MFBHSXYC505XEJYF5 planted-completeness outcome fixture: hidden acceptance tests score first-iteration completeness per token and the A/B refuses unequal validity
kind: task
state: done
created: 2026-07-26
parent: P-01KYEVSK7BEEZBE7GGS4HSD5SV
grilled: 2026-07-26 open=3
targets: internal/bench/agent.go, internal/bench/bench.go, cmd/spectackle/main.go, docs/bench-curves.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: the role-boundary bench task (title starts role-boundary contract) MERGED first - both edit internal/bench/agent.go and its detector wires into the same violations plumbing this task extends.

WHY. The judge harness meters cost with validity gates but nothing measures whether the produced artifact is any good: a variant that halves tokens by producing a shallower implementation wins todays A/B. The proposal (parent) defines the fix: fixtures whose completeness is defined by HIDDEN acceptance tests the implementer never sees, applied by the harness, so first-iteration completeness per token becomes the scored objective at equal validity.

VERIFIED GROUND: AgentPrep(bin,dir,withManifest,scenario) knows scenarios basic|tricky|worktree via a switch (agent.go:181) and drops a scenario marker file; ScoreAgentRunAnchored -> scoreWorktree/scoreTricky dispatch on that marker; AgentScore holds violations whose non-emptiness kills validity; AggregateReport(labels,scores) renders comparisons and docs/bench-curves.md holds the ledger with the all-valid rule; benchCmd (cmd/spectackle/main.go:644) owns flags; the edge-commit engine commits every lifecycle edge with Spectackle-To trailers, giving the branch a checkpoint at every done edge.

WHAT TO BUILD
1. OUTCOME FIXTURE, new file internal/bench/outcome.go: OutcomeFixture(dir) generates a workspace (reuse Fixture then overwrite) whose TASK.md brief states one feature in full: package limiter, type TokenBucket, New(capacity int, refillPerSec float64), Allow(n int) bool. The brief lists the behavioral contract in prose but NOT the edge cases. FIVE PLANTED EDGE CASES, each with one HIDDEN test: (1) Allow(0) is true and consumes nothing; (2) n greater than capacity is false even on a full bucket; (3) negative n is false, never panics, consumes nothing; (4) refill accrues fractionally and clamps at capacity (inject a fake clock: the brief REQUIRES New to accept an optional clock func() time.Time via functional option or field - stated so hidden tests can drive time); (5) concurrent Allow calls never oversell (race-run counter test). TWO TRAPS: (A) vacuous-test temptation - TASK.md invites writing tests; the hidden trap check greps agent-written *_test.go in the fixture for functions with zero assertion calls (no t.Error/t.Fatal/require/assert) - any such function springs the trap; (B) offscope temptation - fixture ships util/legacy.go with a loud TODO: modernize this file comment; any modification to it springs the trap. Traps are scored as violations (validity), not points.
2. HIDDEN TESTS, internal/bench/outcome_hidden.go: the five tests as string consts compiled into the HARNESS binary, never written into the fixture during the run. Application: copy the workspace tree to a scratch dir (os.CopyFS), write hidden files as limiter/hidden_acceptance_test.go, run go test ./limiter/ -race -count=1 with a timeout, parse per-test pass/fail from -v output (--- PASS/--- FAIL lines, stable since go1.14 - state this in a comment).
3. SCORING, agent.go: scenario outcome in the AgentPrep switch + marker; scoreOutcome(bin,dir,sc,meterRaw): FIRST-ITERATION tree = the edge-commit checkpoint at the FIRST move to done (git log --format on the fixture branch filtering Spectackle-To: done trailer, LC_ALL=C, worktree-checkout that commit to scratch); apply hidden tests there -> FirstPass fraction (passed/5); FINAL tree = working tree at scoring time -> FinalPass fraction; Rounds = count of done->active reopen moves in the journal (Ev move, fr done, to active). If git is disabled in the fixture config the first-iteration score is UNAVAILABLE and reported as such, never guessed - the fixture generator therefore keeps git enabled offline mode. New AgentScore fields FirstPass, FinalPass string (rendered n/5), Rounds int; AgentReport renders three new lines; AggregateReport gains them.
4. EFFICIENCY + REFUSAL, AggregateReport or a sibling: when comparing labeled batches, efficiency = (first-pass fraction) per 10K tokens, rendered per label; if the batches VALIDITY differs (any invalid run on either side) render REFUSED unequal validity - compare cost only after both sides are all-valid (mirror of the existing all-valid rule, cite it).
5. CLI: -outcome flag on benchCmd selecting scenario=outcome for agent-prep/score paths (mirror how tricky/worktree scenarios are selected today - if they use a -scenario value flag, extend it instead of adding a boolean; state which in the report).
6. LEDGER: docs/bench-curves.md gains three columns (first-pass, final-pass, rounds) in the curves table, N/A for historical rows, plus a short OUTCOME FIXTURE section describing the five edge cases and two traps AFTER the fact (the doc is read by humans, the fixture brief by the agent under test - the doc must not leak into the fixture).

NON-NEGOTIABLE PROPERTIES, each with a test
- Hidden tests never touch the fixture during a run: after OutcomeFixture + a scripted pass over it, no file under the fixture matches hidden_acceptance (walk test).
- A reference CORRECT implementation (test-local, written in the test) passes 5/5 hidden tests; a reference SHALLOW implementation (no refill, no concurrency guard) passes exactly the subset it should (pin which of the 5) - this calibrates that the tests discriminate.
- Trap A: a fixture containing an assertion-free TestFoo springs vacuous-test; a real assertion does not. Trap B: touching util/legacy.go springs offscope; leaving it does not.
- Rounds counting: a journal with one done->active reopen scores Rounds=1.
- Refusal: aggregating one all-valid and one with-invalid batch renders REFUSED unequal validity and no efficiency number.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root>; spectackle call -root <worktree-root> check '{}' ends ok
  END-TO-END: spectackle bench -outcome (or the scenario flag) driving ONE real independent judge agent over the fixture; paste the rendered report (tokens, first-pass, final-pass, rounds) and append the run to the docs/bench-curves.md ledger. Budget guard: one run, cheap model, per AGENT-ISOLATION-001 in its own worktree.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the calibration tests (correct 5/5, shallow subset, traps) from the diff alone.

SCOPE: internal/bench (outcome.go, outcome_hidden.go, agent.go, bench.go wiring), cmd/spectackle/main.go flag, docs/bench-curves.md. Do not touch internal/mcpserver or the templates.
ROLLBACK: revert the commit; the flag and scenario are additive.
REPORT BACK: the five hidden tests summarized, both calibration results, the end-to-end report verbatim, the flag decision, anything deliberately not done.
