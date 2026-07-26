---
schema: v1
---

## P-01KYEVSK7BEEZBE7GGS4HSD5SV outcome-scored benchmarks: hidden acceptance tests in seeded fixtures make first-iteration completeness a measured number per token
kind: proposal
state: approved
created: 2026-07-26
refs: P-01KYESGDWFFMH80ENHNFXMVZE8, P-01KYD9466KEPWBV2RBK7EQM202
grilled: 2026-07-26
targets: internal/bench/bench.go, internal/bench/agent.go, docs/bench-curves.md

USER REQUIREMENT (2026-07-26): benchmarks must put token usage in relation to LLM RESULT quality. The mission is fully autonomous spec-driven development whose result is valid AND complete - no edge cases or bugs surviving the first iteration - inside the mechanical git workflow, with the LLM spending tokens only on judgment work: state traversal, task elaboration, code, research, validation, refutation. Writing and running tests during implementation is allowed LLM work; composing git/forge commands never is.

WHAT THE HARNESS MEASURES TODAY, and the gap: fixture v3 plus the judge-agent stage meter tool calls and bytes with validity gates and tamper evidence (nonce anchors, journal write deltas), and the reference curves in docs/bench-curves.md hold cost per scenario. Nothing measures whether the PRODUCED ARTIFACT is any good: a variant that halves tokens by producing a shallower implementation currently wins the A/B. Cost without outcome optimizes the wrong axis.

DESIGN - deterministic outcome scoring, no judge subjectivity where avoidable:
1. PLANTED-COMPLETENESS FIXTURES. Each new fixture carries a task brief enumerating a feature whose correct implementation must survive N edge cases, plus HIDDEN acceptance tests - not visible to the implementer agent, applied by the harness after the run - one per planted edge case (and at least one planted trap: a vacuous-test temptation, an offscope-file temptation). The brief states the feature; the hidden tests define complete.
2. SCORE. Per run: tokens_total (existing meter), first_iteration_pass_rate (hidden tests green after the FIRST done, before any reopen), final_pass_rate, reopen_rounds, review_findings_addressed (from the verdict machinery once the approved chain lands). Efficiency = first-iteration completeness per 10K tokens at equal validity; the A/B verdict refuses to compare runs whose validity gates differ, mirroring the existing all-valid rule.
3. WHAT IT CALIBRATES. The review chains risk-gating break-even (feedback.validate=require threshold) currently rests on ESTIMATED catch rates (30-50 percent); these fixtures measure the real catch rate of grill/validate per variant, and the review-mode economics (single-reviewer sequential lenses vs opt-in panels) become an A/B on the same fixtures instead of an argument.
4. ROLE-BOUNDARY CONTRACT. One EARS rule pinned at the root, composed via rule op=add: the server SHALL perform every mechanical workflow step (branch, commit, push, PR, merge, CI await) itself, and no tool result SHALL instruct the caller to run a git or forge command. The bench asserts it: a scenario transcript containing a git instruction to the agent is a validity failure.
5. TOKEN BOUNDS. Hidden tests live outside the fixture workspace (harness-held), cost zero agent tokens; scoring is harness code; one new bench flag -outcome selects the fixture set; curves table gains three columns (first-pass, final-pass, rounds).

EXIT CRITERION on this repository: one outcome fixture with >=5 planted edge cases and 2 traps runs end to end under the judge harness; the report renders tokens, first-iteration pass rate, final pass rate, rounds; an A/B between two hint variants renders efficiency at equal validity and refuses the comparison when validity differs; the role-boundary rule exists, is anchored, and its bench assertion fails a transcript that tells the agent to run git.

## T-01KYFSN48KEK88SNR9MSJRFWA4 role-boundary contract: the server owns every mechanical git step and the bench fails any transcript instructing the agent to run git
kind: task
state: draft
created: 2026-07-26
parent: P-01KYEVSK7BEEZBE7GGS4HSD5SV
targets: internal/bench/bench.go, internal/bench/agent.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Small, self-contained; lands before the outcome-fixture sibling (both edit agent.go - the lease serializes).

WHY. The user requirement pins the division of labor: the LLM spends tokens only on judgment (traversal, elaboration, code, research, validation, refutation); every mechanical git/forge step is the server's. Today that boundary is convention plus implementation; nothing CONTRACTS it and nothing MEASURES a regression where a tool result starts telling the agent to compose git commands - which would silently tax every session.

WHAT TO BUILD
1. RULE ALREADY EXISTS - do not mint a duplicate: ROLE-BOUNDARY-001 (root) already states the server performs every mechanical git/forge step and no tool result instructs the caller to run git. Your job is BINDING and ARTIFACT: check whether ROLE-BOUNDARY-001 carries an applies binding (find scope=rule / anchors); if unanchored, rule op=edit adding applies go:mcpserver.Server.gitFlowFor and VERIFY the anchor row lands (a ROLE-BOUNDARY-001 go:mcpserver.Server.gitFlowFor line). The bench detector below becomes the rule's verifiable artifact either way - cite ROLE-BOUNDARY-001 in the detector's doc comment.
2. DETECTOR in internal/bench/bench.go: func gitInstructionViolations(transcript string) []string returning one violation per match of (a) imperative-instruction shape (?i)\b(?:run|execute|invoke)\s+`?git\b and (b) command-line shape (?m)^\s*(?:\$\s*)?git\s+(?:add|commit|push|checkout|switch|merge|rebase|pull|fetch|reset|cherry-pick)\b. CALIBRATE FIRST, then pin: run the existing scripted bench (spectackle bench) and the current fixture transcripts, grep the concatenated result bytes for the patterns, and paste proof that legitimate output (g branch, g pr N merged, g records, h stale hints, refusal lines) matches NEITHER pattern. If any legitimate line matches, tighten the regex and record the collision in the report - do not ship a detector that fires on todays clean surface.
3. WIRING: scripted mode - Run() appends the violations to Result.Violations and they flip Valid=false. Judge mode - ScoreAgentRunAnchored appends them to AgentScore violations with the same validity effect (find the existing violations field and mirror its plumbing exactly).
4. TESTS: positive control - a synthetic transcript containing run `git push origin` and a second containing a bare git commit -m line each yield exactly their one violation; negative control - a transcript built from real current-surface records (g branch x, g pr 12 merged abc, g records clean, i/ok/! lines) yields zero; wiring test - a Run result carrying one violation is Valid=false.

NON-NEGOTIABLE PROPERTIES
- The detector never fires on the current surface: proven by the calibration run, pasted.
- ROLE-BOUNDARY-001 anchored, lint 0 findings (rule count unchanged at 83).
- No behavior change outside bench: the server code is untouched by this task.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> - 83 rules 0 findings
  spectackle bench (scripted mode) - valid run, zero git-instruction violations, paste the report tail
  spectackle call -root <worktree-root> check '{}' ends exactly ok
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the negative-control test and the calibration grep from the diff alone.

SCOPE: internal/bench only plus the one root rule. Do not touch internal/mcpserver, cmd, templates, docs (curves ledger untouched - no A/B here).
ROLLBACK: revert the commit; the applies edit reverts via rule op=edit restoring the prior applies set.
REPORT BACK: the calibration grep output, the pinned regexes verbatim, each test result, the anchor line.
