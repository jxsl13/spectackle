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

## B-01KYKQA6N1FDQ9W8ADVHN6CV54 rule op=edit with only id claims ok while writing nothing
kind: bug
state: draft
created: 2026-07-28
targets: internal/mcpserver

Found by worktree-batch judge 3 (2026-07-28), verbatim-compacted: rule op=edit id=API-STATUS-001 with no slot fields silently succeeded (ok API-STATUS-001 api/.spectackle/spec.md) but made no actual change - a no-op journal entry; only resupplying the full EARS field set made the edit take. A call that claims ok while writing nothing violates never-silent and burns a discovery round. FIX: rule op=edit with no recomposition input (none of pattern/system/response/trigger/state/condition/feature/rationale/applies given) refuses: ! ARG E - edit needs at least one slot to change; current: <the rule sentence> - teaching the caller the slots AND showing the baseline in one render. Editing only applies (relink) or only rationale stays legal. TEST: pin the refusal (edit with id only), pin that a single-slot edit (rationale) still succeeds, pin the unchanged-sentence case. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1. SCOPE: one refusal in the rule handler + pins. ROLLBACK: revert.
