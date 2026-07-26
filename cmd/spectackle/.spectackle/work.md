---
schema: v1
---

## B-01KYES20VSFJ1VM7JJ3E6XMJS0 self-restart thrash: 31 exec swaps in 45 minutes, one per poll cycle, instead of one per real source change
kind: bug
state: draft
created: 2026-07-26

Observed live on the resident server (serve.log): exec-replacing count went 4 to 31 between 09:48 and 10:33 while sources changed only during two editing bursts. The watcher appears to find the binary stale on nearly every 35s poll. Suspects, in order: the os.Chtimes backdate-to-buildStart is not surviving or not covering the comparison actually used by BinaryStale; a source file with an mtime in the future of every build (the documented future-mtime-thrash limitation, now observed rather than hypothetical); the rebuild itself touching something the staleness probe reads. Each swap costs a full reindex (~213 files) and a listener drain window. Expected: at most one swap per real source-change burst. Verify: reproduce with a synthetic future-mtime file to confirm or exclude that path; then assert via serve.log that an idle hour produces zero swaps while an edit burst produces exactly one.

## ADR-01KYF58AKGEZ3SDFX53H19P3GR Self-restart rebuild policy for the resident server: today it builds and serves the DIRTY working tree, so mid-implementation the lifecycle runs on unreviewed code (it served the refuted round-1 fix while round 2 was still refuting it). Which policy?
kind: adr
state: done
created: 2026-07-26
decision: committed-only: rebuild from git HEAD, dirty tree never served (recommended)
status: accepted

kind: radio
option: committed-only: rebuild from git HEAD, dirty tree never served (recommended)
option: dirty allowed, but every tool result carries a built-from <sha>-dirty stamp
option: keep as is: dirty builds, nothing stamped
blocks: B-01KYES20Y0E9FRQ8DGPC4YD9NK
choice: committed-only: rebuild from git HEAD, dirty tree never served (recommended)

## B-01KYF7DJATEEDAXXV67GW958AK the committed-only watcher swaps while tool calls are in flight, severing the very edge that moved HEAD
kind: bug
state: done
created: 2026-07-26

Reproduced twice on the resident server within minutes of the committed-only landing: a records-committing edge (done, then archived) moves HEAD; the watcher detects it on the next tick and exec-swaps while the SAME call is still running its CI await; the drain times out at 5s and severs the stream. The archive was cut mid-flight - records said archived, the PR stayed open (recovered via the merge-by-hand escape hatch, PR 114). Every long-running records-committing edge is now self-severing by construction. Expected: the watcher defers the swap while calls are in flight - the server tracks an active-call counter (incremented in the gate, decremented on return) and watchStale skips the swap until it reads zero, retrying next tick; HEAD staleness is not urgent, correctness of in-flight edges is. Verify: a synthetic long call held open across a HEAD move must complete on the old generation, with the swap following within one tick of its return; the E2E exec-swap suite stays green. Landing note: drive this fix through per-call CLI sessions, not the resident - its current generation still severs.
