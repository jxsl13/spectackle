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
state: draft
created: 2026-07-28
targets: internal/mcpserver, docs

Found by the 2026-07-28 swarm-contention benchmark (record swarm-contention v1): two concurrent judges with distinct identities ran work op=start on two approved tasks BOTH declaring targets=[shared.go]; both starts succeeded and both worktrees were live simultaneously (proven by the later submit conflict on shared.go). The documented contract says work op=start auto-claims its item+targets and a prefix-overlapping live foreign lease refuses with ! LEASE E naming the holder (SPX-SWM-003) - the refusal never rendered. Convergence still happened through the git conflict at submit (no lost update - both functions landed), so severity is degraded-guarantee, not corruption. INVESTIGATE: does the start auto-claim actually include file targets (or only the item id / dirs)? Does the lease overlap check compare file paths? REPRO: the fixture recipe in the record note - two approved tasks same file target, two identities, concurrent starts; expected per docs is the second start refusing with the l line. FIX direction: make the auto-claim cover normalized targets and the overlap check catch file-vs-file equality - or, if late-conflict convergence is the intended design for the worktree flow, update SPX-SWM-003 and the lease/work docs to say leases advise but the merge layer arbitrates. TEST: two-identity concurrent-start e2e pinning whichever contract is decided. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.
