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

| Scenario | Calls (min/med/max) | Tool bytes (min/med/max) | Valid | Batch |
|---|---|---|---|---|
| basic | 11 | 1016 | valid | anchored regression batch, 2026-07-26 (n=1) |
| tricky | 22 | 2810 | valid | anchored regression batch, 2026-07-26 (n=1) |
| worktree | 15/17/19 | 1742/1799/2023 | **valid 3/3** | anchored batch, 2026-07-26 |

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
| T-01KYEW memory-to-spec manifest nudge | +0B lifecycle | +180B manifest (once per session) | user directive: standing knowledge must reach the spec, not agent-private memory; one sentence per surface, under the 200B brief cap |
