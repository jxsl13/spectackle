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

## T-01KYK7CBRTFEWVVGZTS7ZYDPH8 drift healer: hash-first rebind by (tilde-stem, CHash); cross-file heals without a hash match audit instead
kind: task
state: active
created: 2026-07-28
refs: B-01KYJB3SGKFA2R6PYVE1Y0PP74
grilled: 2026-07-28 open=1
targets: internal/drift, internal/mcpserver, docs

Implements the B-01KYJB3SGKFA2 design pass. INCIDENT: when main.go moved to the module root (PR 177), the bare go:main.main ID re-bound by walk order to examples/saxpy/main.go; Classify computed the impostor span, classed Evolved, and the healer silently crossed files while the true target (unchanged hash, now go:main.main~2) existed. PRINCIPLE: the content hash is the anchor identity; node IDs are lookup hints - never trust an ID binding that crossed files, and never trust a tilde numeral at all. DESIGN: (1) internal/drift gains rebindByHash(ws, g, a) (graph.Node, bool): stem = a.Node with any ~N suffix stripped; candidates = g.Find(stem, 32, KUnknown) filtered to ID == stem or strings.HasPrefix(ID, stem+"~"); for each, SpanHash over its span; return the SINGLE candidate whose hash equals a.CHash - two or more matches is ambiguity, refuse to guess (not found). (2) Classify default branch triggers re-resolution in exactly two cases: node not found (today: Gone), and node found but n.File != a.File with span hash != a.CHash (the crossed-ID case). Success: continue classifying with the candidate (naturally lands Moved when only position changed) and set a new Result.NewNode field so the caller can re-point the anchor. Failure in the crossed-ID case: new class CrossFile - audited, NEVER healed - rendered d audit <rule> <node> <anchorfile> crossfile now=<otherfile>; failure in the not-found case stays Gone. (3) tools.go check: the Moved refresh also writes r.NewNode into a.Node when set (rebind = node ID changed); the CrossFile case joins the tightened|diverged audit arm (never-healed d audit render, counted in audit=). (4) docs/tools.md check section: one sentence - anchors re-resolve by content hash when their node ID crossed files or renumbered; cross-file candidates without a hash match audit, never heal. TESTS (internal/drift fixture with TWO main packages, per the bug repro): (a) move the anchored main - rebind by hash keeps the anchor on the true file, class Moved, NewNode set, heal path updates Node+File+span; (b) remove the true target so only the impostor remains - CrossFile audit, anchor untouched; (c) renumbering: anchor holds stem~2, graph reassigns the node the bare stem - rebind by (stem, CHash) succeeds; (d) ambiguity: two identical-hash candidates - refuse to rebind (not found path). Plus a mcpserver check-flow test asserting d audit ... crossfile renders and audit= counts it. VERIFY: go build ./... && go test ./internal/drift/ ./internal/mcpserver/ -count=1 && gofmt -l . empty. SCOPE: no node-ID format change (the path-qualified-ID alternative is REJECTED: it ripples through anchors, find, graph and every stored record for a problem hash-first resolution fixes without migration). ROLLBACK: revert.

## B-01KYK7W45HF54VFZNG5ZV2M0SN index staleness probe is deletion-blind and new files rescan only via anchored-file staleness
kind: bug
state: draft
created: 2026-07-28
targets: internal/mcpserver

FOUND implementing T-01KYK7CBRTFEW: (1) deleting a source file never triggers a graph rebuild - the staleness probe keys on the newest .go mtime and a deletion bumps nothing, so ghost nodes for deleted files persist until an unrelated edit lands (the in-process e2e saw both old main nodes after os.Remove; a fresh process rebuilt correctly). (2) A NEWLY created file is only indexed when some ANCHORED file also looks stale: the rescan trigger (anchorsNeedRefresh/staleFile) watches anchor files, and the whole-tree rebuild that would index the new file never fires otherwise - find scope=code missed a re-created file until the test touched the anchored main.go. Repro: both are pinned as workarounds inside TestCheckRebindsAndAuditsCrossFile (the two re-touch WriteFile calls with comments naming this bug) - removing those touches reproduces. Expected: the staleness verdict incorporates a tree signature that changes on deletion and creation (e.g. file count + newest mtime, or a dir-mtime walk), so check after a delete drops ghost nodes and check after a create indexes the file without an unrelated touch. SCOPE: the staleness probe only, not the scanner.
