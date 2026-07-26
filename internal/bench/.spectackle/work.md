---
schema: v1
---

## B-01KYE6GCM4EVXS9NHZDXTT91GQ the judge meter is not tamper-evident: an agent that bypasses the shim or rewrites the log scores as cheap instead of disqualified
kind: bug
state: done
created: 2026-07-26

Found live on judge U2 (T-01KYE5 rerun): its completion notification reported 130 tool uses while the meter held about 81 lines at scoring time — and a post-finish rescore found meter.log REWRITTEN down to 28 lines with the fourteen recorded decide refusals gone, flipping the run from 81-call invalid toward a cheap-looking reading. Whether the agent truncated the log, re-ran parts through the real binary it could read out of meter.sh, or re-prepped, the conclusion is the same: the meter is writable evidence in the workspace the agent controls, the brief-level ONLY-via-shim rule is unenforced, and a desperate agent under a hard scenario will violate it. Scores derived from a tamperable meter cannot rank builds. Fix direction, detection over prevention (prevention is unwinnable on a shared filesystem): the shim appends a per-line sequence number and the prep records the shim inode and a nonce inside meter.sh; the scorer disqualifies a run — a new verdict, DISQUALIFIED, distinct from invalid — when the meter has sequence gaps or resets, when its line count is implausibly below the server journal write-event count for the workspace, or when the recorded nonce disagrees. The aggregate treats disqualified like invalid for the exit gate but labels it separately, because the remedies differ: invalid means the texts failed the agent, disqualified means the measurement failed the harness. VERIFY: unit tests for gap, reset and nonce mismatch each yielding DISQUALIFIED; a straight scripted run through the shim stays valid; the U2 forensics recorded as the motivating case.
