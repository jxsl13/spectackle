---
schema: v1
---

## B-01KYEAE3A8FPMSHJD4KSJ2WGZM the meter shim forwards every subcommand, so an agent can re-prep its own workspace and reset the tamper evidence consistently
kind: bug
state: done
created: 2026-07-26

Observed on live worktree judge W1: its meter moved backward from fifteen to eleven lines during the run and finished at fourteen with forty-one agent tool uses, yet none of the B-01KYE6G checks fired — sequence contiguous, nonce matching, journal delta within bound. The consistent explanation: the shim passes its ENTIRE argv to the real binary, so <shim> bench -agent-prep DIR re-runs prep, which rewrites meter.sh with a fresh nonce, resets the journal baseline, and re-seeds — a full evidence reset that leaves every integrity check green. The brief-level never-call-another-binary rule does not help because the reset travels through the sanctioned shim itself. Fix: the shim refuses any first argument other than call (one guard line before the passthrough, exit 2 with a refusal naming the restriction), and prep additionally records its own generation stamp in the scenario sidecar so the scorer can flag a workspace whose prep artifacts are newer than its oldest meter line if a future bypass appears. VERIFY: unit test drives the shim with bench and manifest argv and asserts refusal without execution; a re-prep attempt through the shim in the tamper test yields the refusal; the existing three tamper shapes stay covered.
