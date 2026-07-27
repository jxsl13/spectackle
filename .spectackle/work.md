---
schema: v1
---

## T-01KYJ5FAP6FW3RF5BHD640YC4F mode-parity render diet: online green-path edges compress to one line each - the LLM transitions, the server narrates nothing
kind: task
state: draft
created: 2026-07-27
grilled: 2026-07-27 open=0
targets: internal/mcpserver/gitflow.go, docs/bench-curves.md

USER PRINCIPLE (2026-07-27, RENDER-PARITY-001): online and offline SHOULD cost the LLM the same tokens - PR texts and git texts derive from records; the git machinery is MCP automation; the LLM only performs state-machine transitions. The offline collapse measured -198 tokens per lifecycle, which is exactly the automation narration the old offline read to the LLM - and todays ONLINE green path still narrates it: activation renders g branch X pushed + g records + g pr N draft URL (3 lines), done renders sync lines + g local gates passed + g pr N ready URL + checks-pending + checks-passing (5 lines), archive renders branch arms + records + draft-flip + pin + await + merged (up to 7). DIET: each successful online edge renders ONE line naming its outcome artifact - activation: g pr N draft URL (the URL is the artifact; branch+records collapse into it), done: g pr N ready URL (gates passing is implied by reaching ready; the await verdict folds into the same line when green), archive: g pr N merged SHA. KEEP full verbosity for: every refusal and failure (! GIT E, ! GATE E, ! CI E, checks failing with URL), the divergence w-line (a warning), gitCommitRecords failure lines, and the compensation surface - never-silent means failures speak, not that success narrates. CONTRACTS UNTOUCHED: the merged line stays (ORCH-SYNC-001s anchor), flowAttemptedMerge substring behavior must keep matching (it keys on pr/records/GIT E - verify each kept line preserves the guards semantics; the records line disappearing from the green path means flowAttemptedMerge needs its records match REPLACED by the new single-line vocabulary before the old line goes - do this in the same commit and pin it with the compensation e2e). MEASUREMENT (EVOLUTION-001): scripted bench covers offline only (unchanged by this diet); the online surface cannot run hermetically in the spawned-binary bench (no forge server), so the diet is measured by a LINE-PARITY unit test - a full green online lifecycle through the hermetic in-process fixture (writeOnlineGitConfig + forgeOverride) renders at most one g-line more per edge than offline (the PR URL) - plus before/after byte counts of the e2e outputs recorded in the ledger. TEST: the parity unit test; every gitflow e2e updated to the new lines; the stranded-closure and compensation tests keep passing (failure surface untouched); rolebound negative control gains the new single lines. VERIFY: go build ./... && go test ./internal/mcpserver/ ./internal/bench/ -count=1 && gofmt -l . empty. SCOPE: gitflow.go render strings + flowAttemptedMerge vocabulary + tests + ledger row. ROLLBACK: revert.

## B-01KYJB3SGKFA2R6PYVE1Y0PP74 drift healing crossed an ambiguous node ID to an unrelated file, and tilde-suffixed node IDs are walk-order fragile
kind: bug
state: draft
created: 2026-07-27
targets: internal/graph, internal/mcpserver/tools.go

FOUND by cross-val-mainmove (PR 177): when main.go moved to the module root, the CLI-001 anchor (go:main.main) auto-healed to examples/saxpy/main.go - an unrelated example modules func main whose node won the bare go:main.main ID by walk order; the real CLI main got go:main.main~2. The heal silently CROSSED FILES: the rules anchored code no longer exists at the anchor. Re-stamping via rule op=edit applies=go:main.main picked the example again (same ambiguous resolution); only the explicit tilde form pinned the right node. TWO DEFECTS: (1) the healer treats a node-ID match as identity even when the FILE changes to an unrelated path while a same-hash candidate exists elsewhere - healing should prefer the candidate whose content hash matches the stored CHash (the CLI mains hash was unchanged - the correct target was findable) and AUDIT instead of healing when the best match crosses files without a hash match; (2) tilde suffixes are walk-order dependent, so go:main.main~2 in anchors.tsv can silently renumber when another main package appears - anchors carrying a tilde form need either a stable disambiguator (path-qualified node IDs for main packages, e.g. go:main[examples/saxpy].main) or a load-time re-resolution by (ID-stem, CHash). REPRO: two main packages, anchor on the second, remove/move the first - the anchor renumbers. TEST: fixture with two main packages - heal after a move keeps the anchor on the hash-matching node; a cross-file heal without hash match audits instead. VERIFY: go build ./... && go test ./internal/graph/ ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: grilled - the healer preference + the disambiguator design need a design pass before code. ROLLBACK: revert.

## P-01KYJMVX2QES89YTP3KXSJPA7J first-class benchmark records: unique frame-keyed entries, arbitrary units with direction, depth-1 versioned history
kind: proposal
state: draft
created: 2026-07-27
needs: ADR-01KYJMWEWQE48T3PR76TYQRD3H
targets: internal/mcpserver/tools.go, internal/workspace/workspace.go, internal/journal/journal.go, docs/tools.md

USER REQUIREMENT (2026-07-27): a new benchmark TYPE - benchmarks compare implementations (new/old, Go vs Python, variants) on a FRAME SYSTEM (minimum os/arch/cpu/ram/gpu; implementation dims like cuda/vulkan/simd legal), with arbitrary comparable UNITS (ops/s, alloc/op, token/s), UNIQUE KEYS derived from name+frame, versioned with DEFAULT retention 1 (latest is what the codebase cares about) and a config knob to raise it. DESIGN (three-reader recon + two independent designs + adversarial synthesis, wf_0ed39152; the full spec lives in the workflow journal and is the implementers source of truth): STORAGE - server-owned .spectackle/bench.ndjson per context, union-merged (gitattributes gains the line via ensureLines), keyed last-writer-wins map with bounded per-key history, whole read-modify-write under root.Lock with the read inside the lock, temp+rename rewrite; context discovery allowlist gains bench.ndjson; NO schema bump (additive file). SCHEMA - Record{ID M-prefixed mint per version, Name, Key (stored AND recomputed/verified at load), Ver monotonic per key, Frame map with REQUIRED os/arch/cpu/ram/gpu (sentinels none=absent any=irrelevant), Metrics[]{name,unit,dir +|-|~,noise}, Impls[] ordered, T, Ag, Tool, Note}; metric model over unit-only (two measurements may share a unit); units byte-compared, never converted. KEY - canonicalized sorted k=v dims (folded case, forbidden separators), name folded; the key IS the identity - no deterministic-ID minting (rejected: fixed-epoch seeds violate the ids package contract and lie in Time()). VERSIONING - Ver increments per key on content change; idempotent replay renders unchanged; history trimmed to benchmarks.history (default 1); the PUT-TIME DELTA against the outgoing head is journaled (better/worse/tie per shared impl-metric under its direction, noise-aware ~) so regressions survive trimming - the single best idea of the query-first design. TOOL - one bench tool, ops put|get|ls|rm|cmp, dense k=v/colon grammars never JSON, render prefix m with f/u/d sublines, winner star per metric, RENDER-PARITY-001 one-line ok on success; bench IDs resolve via ids.ResolveRecordID outside the item idScope. FIND - scope=bench via the FTS feed (name, key, frame, impl labels, note indexed). COMPACTION - bench.ndjson is not the journal (no fold interplay); the journaled put-deltas ride EvBench... (a new event kind in the keep-list) or EvArchive-class retention - implementer picks with a fold test. TWO USER DECISIONS PENDING (ADR round): the any sentinel for machine-independent benchmarks, and whether superseded raw values ride the journal or only the delta summary. CHILD TASKS at approval: (1) internal/benchmark package (record, key, store) with unit tests incl. canonicalization/collision/trim matrices; (2) the bench tool + renders + find scope + docs; (3) migration of docs/bench-curves.md ledger entries into first-class records as the acceptance fixture (the offline-collapse A/B and the judge batches become bench records - dogfood). VERIFY per task: go build ./... && go test ./... -count=1 && gofmt -l . empty; e2e: put/get/ls/cmp/rm round-trip, depth trim, union-merge conflict load, idempotent replay, fold survival of put-deltas. ROLLBACK: revert; the ndjson file is additive.

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
state: submitted
created: 2026-07-27
status: proposed

kind: radio
option: summary only - raw superseded values are destroyed
option: raw values ride the journal event too
blocks: P-01KYJMVX2QES89YTP3KXSJPA7J
