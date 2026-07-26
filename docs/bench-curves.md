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

| Scenario | Calls | Tool bytes | Valid | Batch |
|---|---|---|---|---|
| basic | 11 | 1016 | valid | anchored regression batch, 2026-07-26 (n=1 per scenario) |
| tricky | 22 | 2810 | valid | anchored regression batch, 2026-07-26 |
| worktree | 37 | 5709 | **INVALID (flow)** | anchored regression batch, 2026-07-26 |

The worktree row is a finding, not a baseline: across four live worktree
judges to date, TWO delivered the file change by editing main directly
instead of discovering the `work` flow — the flow column caught both, and
at that rate the shortcut is a guidance gap, not judge variance. The
provisional cost curve for agents that DO find the flow is 16-19 calls /
1823-2369B (first batch). Until a text change makes the flow reliably
discoverable — the natural candidate: the approved-transition result
naming `work op=start item=<id>` as the next step — worktree batches are
expected to carry shortcut invalids and their aggregate exit codes must be
read with that in mind.

All three runs passed the nonce anchor on its first live outing: prep's
printed nonces, held outside the workspaces, matched at scoring with no
DISQUALIFIED.

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
