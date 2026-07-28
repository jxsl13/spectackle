---
schema: v1
---

## ADR-01KYJMWE1NFJ7VZ82GX3YK0FMZ Benchmark frames: os/arch/cpu/ram/gpu are required keys. May a machine-independent benchmark (byte counts, token curves) use the sentinel any (dimension irrelevant) so one key spans hosts, or must every benchmark pin real host values?
kind: adr
state: done
created: 2026-07-27
decision: allow the any sentinel for machine-independent dims
consequences: Machine-independent benchmarks (byte counts, token curves) share one unique key across hosts via any; none stays for genuinely absent hardware; host-dependent benchmarks still pin all five real values. The key canonicalization treats any as a first-class value, and cmp across frames renders the sentinel verbatim.
status: accepted

kind: radio
option: allow the any sentinel for machine-independent dims
option: always pin real host values - no sentinel
blocks: P-01KYJMVX2QES89YTP3KXSJPA7J
choice: allow the any sentinel for machine-independent dims

## ADR-01KYJMWEWQE48T3PR76TYQRD3H Benchmark history at default depth 1: when a new version supersedes the old, what survives? The put-time delta summary (better/worse/tie per metric) is always journaled; should the superseded RAW metric values also ride the journal event (bounded per-put growth, richer regression forensics), or is the summary enough?
kind: adr
state: done
created: 2026-07-27
decision: raw values ride the journal event too
consequences: USER CHOSE the richer option over the lean recommendation: every put that supersedes a version appends the outgoing versions full metric values to the journaled delta event - bounded per-put growth, full regression forensics at depth 1. The put event schema carries prior impl/metric values alongside the better/worse/tie summary; compaction keeps the event class.
status: accepted

kind: radio
option: summary only - raw superseded values are destroyed
option: raw values ride the journal event too
blocks: P-01KYJMVX2QES89YTP3KXSJPA7J
choice: raw values ride the journal event too

## B-01KYKSKMHNE2HS9H235BG6DV4B work op=start does not enforce the target lease against a live sibling worktree
kind: bug
state: active
created: 2026-07-28
targets: internal/mcpserver, docs

Found by the 2026-07-28 swarm-contention benchmark (record swarm-contention v1) and REPRODUCED DETERMINISTICALLY by its honesty validator in a fresh repo: two approved tasks both declaring targets=[contended.go], two identities, sequential work op=start while the first worktree stays live - the second start SUCCEEDS, and lease op=ls is EMPTY right after the first start. So this is not an overlap-check miss: the start auto-claim creates NO lease for file targets at all, despite the documented contract (work op=start auto-claims its item+targets; prefix-overlap of a live foreign lease refuses with ! LEASE E naming the holder, SPX-SWM-003). Convergence still happened through the git conflict at submit (no lost update - both functions landed), so severity is degraded-guarantee, not corruption. FIX direction: make work op=start actually claim normalized targets (the lease machinery exists - explicit lease op=claim works) so the second start refuses per the docs - or, if late-conflict convergence is the intended worktree-flow design, retire the auto-claims-targets sentence from SPX-SWM-003/the work docs and document the merge layer as the arbiter. This is a real design decision: the lease path saves the loser judge the whole wasted implementation (refuse at start), the conflict path costs a full implement-then-resolve round but never blocks. TEST: two-identity concurrent-start e2e pinning whichever contract is decided. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.

## ADR-01KYKTGGPREG2B7XJ1FTY25E7S Worktree contention: enforce the lease at work op=start, or keep merge-layer arbitration?
kind: adr
state: done
created: 2026-07-28
context: The swarm-contention benchmark (M-01KYKSKKPDFNT, B-01KYKSKMHNE2H) proved work op=start creates NO file-target lease despite SPX-SWM-003 documenting an auto-claim: two concurrent agents on the same declared target both start, both implement, and the slower one pays a full implement-then-resolve round at submit (measured ~20 calls wasted vs 1 refused call). Convergence is safe either way - zero lost updates. The choice is the coordination contract.
decision: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)
status: accepted

kind: radio
option: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)
option: warn: start renders the l-line naming the holder but proceeds - informed parallelism, the second agent chooses
option: redocument: leases stay advisory for the worktree flow; SPX-SWM-003 and work docs updated to name the merge layer as arbiter - never blocks
blocks: B-01KYKSKMHNE2HS9H235BG6DV4B
choice: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)
