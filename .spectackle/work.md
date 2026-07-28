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

## B-01KYKMPAFNEW39VEGPBTKX38MG the archive-time VALIDATE W reads ambiguous - advisory or actionable
kind: bug
state: draft
created: 2026-07-28
targets: internal/mcpserver

Two of three v0.6.2 outcome judges independently flagged the same confusion, verbatim-compacted: move to=archived prints ! VALIDATE W <id> no validation verdict - validate op=verdict from a second identity, then archives anyway - it wasnt clear whether this was a soft advisory or something I needed to resolve first; the wording (W vs E) wasnt obvious until the archive still went through. The warning states the demand but not its softness or its purpose. FIX: one line change - the W text gains the consequence clause: no validation verdict recorded (advisory: archive proceeds; a verdict from a second identity closes the audit trail - required only when feedback.validate=require or the risk gate trips). Pin the text in the archive-warn test. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1. SCOPE: one warning string + pin. ROLLBACK: revert.
