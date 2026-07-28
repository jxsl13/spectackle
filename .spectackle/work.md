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

## P-01KYMCKE8DEW7BZ3FNCMJTNSG2 knowledge conflict resolution is unreachable from the tool surface
kind: proposal
state: approved
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
grilled: 2026-07-28 open=0
targets: internal/knowledge, internal/mcpserver/knowledge.go

Capability gap from the R-01KYMA7EXME6K gap hunt (WARN 6), empirically confirmed: internal/knowledge implements Resolve/Apply so a human can pick a winning decision and carry it forward with the loser preserved, but NO MCP op reaches it - knowledge accepts export|merge|apply only. Consequence measured: merge honestly reports conflicting ADRs as x lines and EXCLUDES them from the condensate, so applying that condensate lands neither side and both decisions vanish from the target; the only way to carry a curated outcome forward is hand-editing the artifact markdown, defeating the server-is-the-only-writer model. DECIDED SHAPE (ADR-01KYMKEG7YE2P, user): decide-integration - each conflict mints an ADR in the APPLYING workspace and answering it selects the winner. Reuses ASK-SURFACE-001 and the existing decide UI, adds no second decision channel and no new grammar; the cost is that it is the heaviest of the three options. Rejected: a knowledge op=resolve op (smallest surface but a second decision channel beside decide), and document-only (zero code but leaves the documented promise that curation is a humans call with no call). DESIGN CONSEQUENCE the implementer must respect: merge strips conflicts from the condensate, so apply of a single condensate can never see them - apply therefore accepts MULTIPLE artifacts (the same paths+body inputs merge takes), detects conflicts itself via knowledge.Merge, applies the non-conflicting union exactly as today, and mints one ADR per conflict rather than silently dropping it. Single-artifact apply keeps todays behavior byte for byte. CHILD TASK at approval: T-01KYMN (the wiring).

## ADR-01KYMKEG7YE2PS8DSJZJW799P9 knowledge merge reports conflicts but no op can resolve them — which shape should resolution take?
kind: adr
state: done
created: 2026-07-28
context: The gap hunt proved (P-01KYMCKE8DEW7) that internal/knowledge implements Resolve/Apply so a human can pick a winning decision and carry it forward with the loser preserved, but no MCP op reaches it: knowledge accepts export|merge|apply only. merge honestly reports conflicting ADRs as x lines and EXCLUDES them from the condensate, so applying that condensate lands NEITHER side and the only way to carry a curated outcome forward is hand-editing the artifact markdown - defeating the server-is-the-only-writer model.
decision: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build
status: accepted

kind: radio
option: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build
option: knowledge op=resolve key=<conflict key> choose=<source> - a direct op writing the winner plus a resolution block into the condensate; smallest new surface, but a second decision channel beside decide
option: document-only: state that conflicts are deliberately excluded and curation happens outside the tool; zero code, but the promise that curation is a humans call keeps having no call
blocks: P-01KYMCKE8DEW7BZ3FNCMJTNSG2
choice: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build

## T-01KYMPN0PNEWVS330NKPSQNRDT knowledge apply mints an ADR per conflict instead of dropping both sides
kind: task
state: draft
created: 2026-07-28
parent: P-01KYMCKE8DEW7BZ3FNCMJTNSG2
grilled: 2026-07-28 open=0
targets: internal/mcpserver, docs

Implements ADR-01KYMKEG7YE2P (decide-integration) for P-01KYMCKE8DEW7. TODAY: knowledgeApply reads ONE artifact (in.Path or in.Body) and folds it in; merge reports conflicts as x lines and excludes them from the condensate, so a conflicting decision can never reach a target workspace at all. CHANGE, in internal/mcpserver/knowledge.go: (1) knowledgeApply accepts the SAME inputs merge takes - in.Paths (list) alongside the existing in.Path/in.Body - and when more than one artifact is supplied it runs knowledge.Merge over them, applies the merged condensate through the EXISTING additive-idempotent path unchanged, and then, for each returned Conflict, mints one ADR in the applying workspace. (2) Each conflict ADR is minted through the SAME primitives decide.go resolveDecision uses (lifecycle.Draft kind=adr + the decide options/journal path) - never a new write path: question = the conflicting entry question, context names the artifact sources and the competing decisions verbatim, options = the distinct decisions (one per source, labeled with its source so the answer is unambiguous), item = empty. (3) The render gains one need decision <ADR-id> <question> line per conflict, exactly the shape decide op=ask emits headlessly, so the caller sees what to answer; the ok applied trailer additionally reports conflicts=<n>. (4) Answering that ADR via decide op=answer applies the winning decision as a real ADR in the workspace: extend resolveDecision (or a small sibling) so an answer whose ADR carries a knowledge-conflict marker writes the chosen decision through the same ADR-apply path knowledgeApply already uses for non-conflicting ADR entries. Keep the loser recoverable: the conflict ADR body records BOTH sides with their sources, so the journal tombstone preserves what was not chosen. (5) SINGLE-artifact apply must stay byte-for-byte identical - guard the new branch on len(artifacts) > 1. TESTS: (a) two artifacts with the same rule and no conflict - applied once, added= honest, no ADR minted; (b) two artifacts whose ADRs answer the same question differently - the non-conflicting union applies, exactly one ADR is minted per conflict, the render carries need decision and conflicts=1, and NEITHER decision is silently applied; (c) answering that ADR with one sides decision lands that decision as a real ADR in the workspace and the loser is still readable in the journal; (d) single-artifact apply unchanged (pin the existing trailer); (e) idempotency - re-applying the same two artifacts after the answer mints no duplicate ADR. VERIFY: go build ./... && go test ./internal/mcpserver/ ./internal/knowledge/ -count=1 && gofmt -l . empty. DOCS: the knowledge section gains the multi-artifact apply and the conflict-to-ADR flow. SCOPE: internal/mcpserver/knowledge.go (+ decide.go only if the answer path genuinely needs a hook), tests, docs. ROLLBACK: revert.
