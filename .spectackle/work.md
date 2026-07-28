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

## T-01KYKGZT0SEQS8S6J12XJKSA58 check surfaces vacuous tests in dirty test files - the in-loop version of the validate detector
kind: task
state: draft
created: 2026-07-28
targets: internal/mcpserver, docs

DATA (outcome batches M-01KYJWG08TFRC v1+v2): validity is stuck at 1/3 because judges reach check ok and done with assertion-free tests; the AST vacuous-test detector exists but only in validate computed pack (post-diff) and agent-score (post-hoc) - nothing surfaces it while the agent is still in its edit loop, and the orchestrator/judge sees it only after the run is voided. FIX: check gains a warning class over the WORK IN FLIGHT: for each wt.DirtyFiles(s.ws.Dir) entry ending _test.go, run the existing vacuousTestLines AST detector (validate.go ~469 - subtests without assertions, unguarded ranges holding all assertions; NOT a fakeable word-check, which the rejection corpus killed - this reuses the accepted mechanism verbatim) and render one finding per hit: ! VAC W <file>:<line> <reason>, capped at 10 with a +n more tail exactly like validate does. Scope rationale: dirty files = the current edit loop, so brownfield repos with committed legacy tests stay quiet and check ok stays reachable; DirtyFiles errors (non-git workspace) skip silently. Placement: in the check handler after the drift block, before compact candidates. Docs: tools.md check section one sentence + VAC joins the ! code list in the output grammar. TESTS: (a) a connectRoot fixture writes a dirty _test.go with an assertion-free subtest - check renders ! VAC W with file:line; (b) git-committing the file silences it (dirty-only scope pinned); (c) a test file whose subtests all assert stays quiet; (d) the cap: 12 vacuous subtests render 10 + the +2 more tail. MEASUREMENT: the next outcome judge batch on the released binary A/Bs validity against v2 1/3 - hypothesis: in-loop visibility moves the vacuous-test class from post-hoc void to pre-done fix. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: check handler + docs + tests; the detector itself is untouched. ROLLBACK: revert.
