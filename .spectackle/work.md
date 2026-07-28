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

## R-01KYMA7EXME6KAW9B77MJQ4MSD adversarial gap hunt on the knowledge and compact surfaces - the last tools no independent agent has stressed
kind: research
state: active
created: 2026-07-28
grilled: 2026-07-28 open=0
targets: internal/mcpserver/knowledge.go, internal/knowledge

COVERAGE GAP: every judge batch and benchmark so far drove the lifecycle tools (draft/grill/move/work/check/decide/bench/lease). knowledge (export, merge, apply across repositories) and compact (fold candidates, apply) have NEVER been exercised by an independent agent - all their behavior rests on unit tests written by the same author as the code. knowledge is the higher-risk half: it moves rules and ADRs BETWEEN workspaces, has conflict semantics that must never auto-resolve (same question, different decision renders x lines), an additive-idempotent apply that dedups on content key rather than rule ID, and a brownfield entries path for repos with no bundle. BRIEF for the delegated agent: build a THREE-repo fixture from scratch (A: rich cascade - several EARS rules in different context dirs, at least two ADRs with recorded decisions, a couple of archived items; B: brownfield, plain git repo with source files and NO .spectackle; C: divergent - its own bundle whose ADRs answer the SAME questions as A with DIFFERENT decisions). Then stress, recording the exact render of every call: knowledge op=export from A (with and without a path); apply that artifact to B and verify what actually landed (rules composed through the real composer, ADRs through the decide path, added=/gaps= counts truthful); apply the SAME artifact to B a SECOND time and verify idempotency (no duplicates, no double-counted added=); merge A+C artifacts and verify conflicting decisions surface as x lines and are NEVER silently resolved; apply a merged condensate and see what happens to the conflicts; the brownfield entries=... path (caller-authored entries validated and keyed via NewEntry); and adversarial inputs - an artifact hand-edited to carry a malformed rule, an empty artifact, an artifact from a repo with a different schema stamp. ALSO compact: with journal_max low, dry-run then apply, and verify the never-dropped classes (reject, archive, compact, escalate, decide, bench, review, validate) actually survive a fold, plus the done-unarchived and mergeable-rule candidate renders. DELIVERABLE: a numbered defect list, each with the exact reproduction command, observed vs expected, and severity; plus any hint or render that misled the agent. Findings become bug items citing this R-id.

## B-01KYMCHNJCFBPSVBB5P4A65JK0 knowledge export stamps the spectackle binary module as source, not the exported repo
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/knowledge, internal/mcpserver/knowledge.go

Found by the R-01KYMA7EXME6K gap hunt (FAIL 1+5), empirically isolated. knowledge op=export writes source: github.com/jxsl13/spectackle for EVERY repo, because moduleRepoURL/debug.ReadBuildInfo reports the RUNNING BINARY module, never the -root workspace. Two unrelated fixture repos exported byte-identical source labels. DAMAGE: (a) merge of two repos artifacts reports sources=1 instead of 2; (b) unionProvenance dedups on (Source,Dir), so a rule present independently in two repos counts 1 - the tools headline feature (genericity measured by recurrence rank, never inferred) silently undercounts whenever two repos share a dir name, which is the normal case for a fleet sharing one installed binary; (c) conflict x lines render both sides with the SAME src=, so a human cannot tell WHICH repo holds which decision - the one fact needed to adjudicate, and the Conflict doc comment explicitly promises both sides intact. FIX: derive source from the -root workspace (git remote origin URL, else the repo directory name), fall back to the module path only when the root has no identity; keep it in the artifact header. TEST: export from two fixture repos - distinct sources; merge - sources=2, a rule in both counts 2, conflict x lines name different srcs. VERIFY: go build ./... && go test ./... -count=1.
