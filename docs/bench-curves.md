# Judge reference curves

The agent-judge stage (`bench -agent-prep` / `-agent-score`, see
`internal/bench`) measures GUIDANCE quality: a fresh, independent agent
drives a generated fixture through the real tool surface with a fixed,
zero-coaching brief, and is scored mechanically — goal states from the
workspace, cost from a tamper-evident meter. A scripted driver cannot get
lost; an agent can, and how far it wanders is the metric every text change
must move or hold.

This document is the durable home of the reference curves and the operating
discipline. Archive notes hold the full history; this table holds the
numbers a text change is compared against.

## Scenarios

| Scenario | Goal set | Scored by |
|---|---|---|
| `basic` | task drafted → archived; bug drafted → rejected with note; check ok | `find scope=history` / `scope=rejection`, check taxonomy |
| `basic -with-manifest` | same, with the connect-time manifest prepended to the brief (simulates an MCP session; manifest bytes reported as a separate session line) | as basic + `manifest.size` sidecar |
| `tricky` | rule with slots under the W001 lint; reopen loop until the server side-steps to blocked; decide `rescope` exit so the task ends draft; check E-free | `find scope=rule`, live state listing, decide-answer call in the meter |
| `worktree` | code change delivered through `work start` → edit under the reported root → `submit` (cross-process reattach); task ends done; check E-free | file content on disk, live listing, exit-0 `work start`+`submit` calls in the meter (`flow=` — a direct edit on main scores invalid) |

## Reference curves

Each entry is one batch: n=3 fresh haiku judges, mechanically scored in one
anchored command, all-valid gate. Bytes are the tool-output diet
(stdout+stderr of every metered call); the manifest, when present, is a
separate per-session figure. Run-to-run noise floor on identical builds:
±2B per scripted A/B run; judge batches carry real variance — quote
spreads, never single runs.

<!-- BASELINE-TABLE: filled from the anchored regression batch of the day
     this document landed; update whenever a text change moves a curve,
     with the batch's aggregate line and date. -->

| Scenario | Calls (min/med/max) | Tool bytes (min/med/max) | Valid | First-pass | Final-pass | Rounds | Batch |
|---|---|---|---|---|---|---|---|
| basic | 11 | 1016 | valid | N/A | N/A | N/A | anchored regression batch, 2026-07-26 (n=1) |
| tricky | 22 | 2810 | valid | N/A | N/A | N/A | anchored regression batch, 2026-07-26 (n=1) |
| worktree | 15/17/19 | 1742/1799/2023 | **valid 3/3** | N/A | N/A | N/A | anchored batch, 2026-07-26 |
| outcome | 32 | 3796 | valid | 5/5 | 5/5 | 0 | anchored run, 2026-07-27 (n=1, sonnet judge, 82K total agent tokens; no traps sprung) |

The worktree row graduated from a finding to this baseline. History of the
graduation, because it is the judge program's best argument for itself:
across the first four live worktree judges, two delivered the file change
by editing main directly — the flow column caught both. The first fix (the
approved-transition result naming `work op=start item=<id>` as the next
step, T-01KYEBT) cut the shortcut rate; the remaining invalid then turned
out to be a REAL product bug, not guidance: the gitflow branch created at
activation collided with the worktree branch name, permanently locking
ever-active items out of the work flow (B-01KYED3D, fixed as attach-not-
create through three adversarial review rounds). With both landed, the
anchored rerun scored 3/3 with zero shortcuts and every edit under its
worktree root — the tightest worktree spread measured.

Nonce anchors have been clean on every anchored batch to date.

Historic context (stitched from archive notes, superseded by the table
above): basic fell from 4677/3846B at 17 calls (zero-doc baseline) to
1038/1119/1135B at 12-14 calls; with the dieted 3473B manifest prepended,
639/715/715B at 8-9 calls. Tricky fell from 3223/3612/4202B at 30-38 calls
to 2360/2568/2982B at 17-21 after the shape-line fixes. Worktree opened at
1405/1823/2369B at 14-19 calls (one run invalid on the flow column — the
scorer catching a main-edit shortcut on its first live outing).

## Outcome fixture (T-01KYFSQQ)

The `outcome` scenario scores the ARTIFACT, not just the run: the brief
states a token-bucket limiter feature (injectable clock mandated so timing
is testable); five hidden acceptance tests — harness-held, never in the
fixture — define complete: zero-consume, over-capacity, negative input,
fractional refill with clamp, concurrent no-oversell. Two traps score as
validity violations: an assertion-free agent test (vacuous temptation) and
any edit to `util/legacy.go` (offscope temptation, TODO bait). First-pass
is the hidden-suite result at the FIRST done edge (tree recovered from the
edge-commit trailer `Spectackle-To: done`); final-pass at scoring time;
rounds counts done→active reopens in the journal. Efficiency renders as
first-pass completeness per 10K tokens, and the comparison REFUSES to
render when any run in the set is invalid — the all-valid rule extended to
outcomes. Calibration is pinned by tests: the reference correct
implementation passes 5/5, the reference shallow one exactly 2/5.

## Outcome A/B: validation warn vs require (2026-07-27, T-01KYGX9P)

First catch-rate measurement replacing the estimated 30-50% band. Four
sonnet judges, config-only variants on the v0.2.0 binary, limiter fixture.

| Run | Gate | Calls | Surface tokens | First-pass | Final-pass | Rounds | Verdict |
|---|---|---|---|---|---|---|---|
| warn-1 | warn | 43 | ~1381 | 5/5 | 5/5 | 0 | valid |
| warn-2 | warn | 44 | ~1220 | 4/5 | 4/5 | 0 | DISQUALIFIED (shim seq race) |
| require-1 | require | 69 | ~2051 | 0/5 | 5/5 | 2 | valid |
| require-2 | require | 144 | ~8928 | 4/5 | 4/5 | 2 | valid |

Efficiency REFUSED (all-valid rule): warn-2's disqualification is a
harness artifact — the meter shim's read-count-then-append sequence is not
atomic under a judge's PARALLEL tool calls, and out-of-order appends read
as tampering (bug filed). Content findings stand:

- The gate catches STRUCTURAL incompleteness: require-1's premature first
  done (0/5 hidden) was fully repaired to 5/5 across the gate's two
  rounds — the reopen loop doing exactly its job.
- The gate does NOT catch SEMANTIC divergence from unstated spec:
  require-2 deliberately made `Allow(n<=0)` a success-no-op (a defensible
  hardening choice) against the hidden refusal semantics and held 4/5
  through both rounds — computed classes see untested/undocumented
  SHAPES, not meaning.
- Cost: require ran 1.5-6.5x the warn surface tokens and 2 rounds each;
  warn's valid run was 5/5 first-pass at 43 calls with no gate at all.
- Provisional catch data: 1 of 2 require runs materially repaired by the
  gate. n is far too small for a rate; the estimated band survives with
  its first real data points and a rerun awaits the shim fix.

## Operating discipline

- **n=3, all-valid gate.** One failing judge in three is a regression
  however good the median looks; the aggregate's exit code enforces it.
- **DISQUALIFIED is not INVALID.** Invalid means the texts failed the
  agent; disqualified means the measurement failed the harness (meter
  sequence gap, nonce mismatch, journal write-event delta beyond the
  measured 3-events-per-write bound, or a nonce-anchor mismatch).
- **Record the nonce at prep, pass it at scoring.** `bench -agent-prep`
  prints `agent nonce <hex>`; `-agent-score dir1,dir2 -nonces n1,n2`
  matches positionally. The anchor is the one piece of evidence a full
  consistent workspace re-prep cannot forge, because it never entered the
  workspace.
- **Judges get the brief verbatim and nothing else**, a pinned
  `SPECTACKLE_AGENT` (per-call processes must share one identity or the
  worktree reattach gate correctly refuses), and the instruction to use
  only the shim — which itself refuses every subcommand but `call`.
- **Scripted A/B first, judges second.** `bench -baseline A -against B`
  answers per-call byte deltas cheaply; judges answer whether guidance
  changed navigation. A change outside the metered surface states so
  explicitly instead of skipping measurement silently.

## Text-change A/B ledger (TOKEN-OBJECTIVE-001)

Every machine-facing text change ships with its scripted A/B delta here;
justification required when the delta is positive.

| change | delta/lifecycle | manifest | justification |
|---|---|---|---|
| T-01KYD94K grill verdict machinery (deleted word-checks, open= stamp) | −95B | +0B | strictly cheaper at equal validity |
| T-01KYD94M3 validate archive gate (warn nudge on unvalidated task/bug archives) | +94B | +0B | the warn IS the validation nudge; it buys prevented correction rounds the scripted bench cannot see — user-approved machinery (ADR-01KYES0TM risk-gated require), catch-rate to be measured by the outcome benchmarks (P-01KYEV) |
| T-01KYD9J per-finding addressal (waivers, legacy-render guard, target-bound validate hash) | ±0B (tie at noise floor; an earlier −40B measurement did not reproduce and is retracted) | +0B | refusal texts replaced like-for-like; the tool-description surface grew +238B one-time list cost for the waivers teaching |
| T-01KYF3 MinRecordPrefixLen 6→13 (ADR-01KYEP) | +55B (~14 tokens) | +0B | user-decided trade: lifetime-stable displayed prefixes end the ambiguous-prefix re-disambiguation rounds (observed twice live); cost accepted in the ADR |
| T-01KYD94MG edge-commit engine (structured decision commits per write) | +126B (~31 tokens) | +0B | the requirement is explicit: the decision trail must be readable in git log by humans, fully automatic, zero LLM git commands; the validator's redundancy dissent is recorded on the task and the default stays edges |
| T-01KYEW memory-to-spec manifest nudge (re-landed via B-01KYFG1KEEF1S) | +0B lifecycle | +180B manifest (once per session) | user directive: standing knowledge must reach the spec, not agent-private memory |
