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
