---
schema: v1
---

## T-01KYJ5FAP6FW3RF5BHD640YC4F mode-parity render diet: online green-path edges compress to one line each - the LLM transitions, the server narrates nothing
kind: task
state: active
created: 2026-07-27
grilled: 2026-07-28 open=0
targets: internal/mcpserver, docs/bench-curves.md

USER PRINCIPLE (2026-07-27, RENDER-PARITY-001): online and offline SHOULD cost the LLM the same tokens - PR texts and git texts derive from records; the git machinery is MCP automation; the LLM only performs state-machine transitions. The offline collapse measured -198 tokens per lifecycle, which is exactly the automation narration the old offline read to the LLM - and todays ONLINE green path still narrates it (surface as of v0.5.5, post PR-DRAFT-001): activation renders g records clean + g branch X pushed + g pr N draft URL (3 lines); done renders g records clean + g pushed X + g local gates passed + g pr N stays draft until archive + g pr N checks pending + g pr N checks passing (up to 6); archive renders records + gates + g pr N ready URL + checks pending + checks passing + g pr N merged SHA (up to 6). DIET: each successful online edge renders ONE line naming its outcome artifact - activation: g pr N draft URL (branch+records collapse into it); done: g pr N draft checks passing (gates passing implied; stays-draft-until-archive implied by PR-DRAFT-001; the CI verdict IS the outcome artifact at done; when checks are still pending after the wait budget the existing ! CI W line stays as-is); archive: g pr N merged SHA (ready flip + gates + checks implied by the merge). KEEP full verbosity for: every refusal and failure (! GIT E, ! GATE E, ! CI W/E, checks failing with URL), the divergence w-line, gitCommitRecords failure lines, the human-flipped notice, and the compensation surface - never-silent means failures speak, not that success narrates. CONTRACTS UNTOUCHED: the merged line stays (ORCH-SYNC-001 anchor); flowAttemptedMerge substring behavior must keep matching - it keys on pr/records/GIT E; the records line disappearing from the green path means flowAttemptedMerge needs its records match REPLACED by the new single-line vocabulary before the old line goes - same commit, pinned by the compensation e2e. MEASUREMENT (EVOLUTION-001): scripted bench covers offline only (unchanged); the online surface cannot run hermetically in the spawned-binary bench, so the diet is measured by a LINE-PARITY unit test - a full green online lifecycle through the hermetic in-process fixture (writeOnlineGitConfig) renders at most one g-line more per edge than offline (the PR URL/SHA artifact) - plus before/after byte counts of the e2e outputs recorded in the ledger and as a bench record (name=online-edge-render, all-any frame, impls narrated vs diet, metric bytes:B:-). TEST: the parity unit test; every gitflow/worktree/offline e2e updated to the new lines; TestSingleReadyFlipAtArchive updated (done asserts the single g pr N draft checks passing line); stranded-closure and compensation tests keep passing; rolebound negative control gains the new single lines if it pins any. VERIFY: go build ./... && go test ./internal/mcpserver/ ./internal/bench/ -count=1 && gofmt -l . empty. SCOPE: gitflow.go render strings + flowAttemptedMerge vocabulary + tests + ledger row + one bench record. ROLLBACK: revert.

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
