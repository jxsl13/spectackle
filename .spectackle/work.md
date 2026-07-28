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
state: active
created: 2026-07-28
parent: P-01KYMCKE8DEW7BZ3FNCMJTNSG2
grilled: 2026-07-28 open=0
needs: ADR-01KYN001T6E2SVBX8ZJ3FGXEPJ
targets: internal/mcpserver, internal/lifecycle, internal/knowledge, docs

Round 4, after rescope (ADR-01KYN001T6E2S). Scope as it now stands: make knowledge apply's conflict curation lossless END TO END. The core is implemented and independently validated across three rounds - conflicts open an ADR instead of dropping both sides, adr/research tombstones retain their substance, entry identity is content-derived, minting is idempotent and race-safe, and a divergence against a held position is reported. What remains is four defects round 3 reproduced, two of them incomplete versions of round 2's own fixes.

F1 CRITICAL, internal/lifecycle/lifecycle.go adrOutcome(): the cap reservation protects only against a large BODY. The four structured fields are joined in fixed order (context, decision, consequences, status) and, when that blob alone exceeds retainedBodyMax, capped from the END - so a large context, which nothing bounds, amputates decision:, consequences: and status: exactly as before the round-2 fix. A ~9k decision drops status: for the same reason. FIX: budget by importance, not by position. decision and status ARE the outcome and are short; give each a generous per-field cap so the must-keep blob is bounded by construction, reserve it first, and let body/context/consequences share what remains. Emit in the canonical order regardless - budgeting order and render order are different concerns.

F2 CRITICAL, same file, isDecideScaffold(): option: is missing from the skip-list, and a knowledge-conflict ADR has at least two option: lines by construction. So an unanswered one archives with its FIRST option as the gist - the spec.md intent line and journal summary read as a confident decision for one side, with no sign the other side existed or that nothing was decided. That is worse than the kind: radio it replaced, which was at least visibly meaningless. FIX: treat option:/choice: as scaffolding too, and make the unanswered gist state the truth - undecided, with the number of options that were on the table.

F3, internal/mcpserver/knowledge.go: settled := len(conflicts) - open silently folds a lock timeout or mint error into settled, so a conflict that FAILED reports as one this workspace had already answered. Reproduced by holding coord.db's lock for the key from another agent: the render carries both the ! ARG E timeout AND settled=1. FIX: count settled explicitly, never by subtraction.

F4, same file, divergedValue(): it reads only Decision (adr) or Text/Prose, so a divergence caused purely by differing Context/Consequences/Status renders ours="X" theirs="X" - two identical strings for a correctly detected disagreement. FIX: when the primary values agree, name the fields that actually differ.

TESTS: (a) a huge context, and separately a huge decision, must still leave decision: and status: readable in the tombstone - the direct regression for F1, which the round-2 test missed by only varying the body; (b) an unanswered conflict ADR's spec.md intent line and journal summary must name neither side as chosen; (c) a conflict whose mint fails is not counted settled; (d) a Context-only divergence names the differing field. Keep every earlier round's test.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && go test ./internal/mcpserver/ -run TestConcurrentApplyMintsOneDecision -count=30 -race && gofmt -l . empty.

SCOPE: internal/lifecycle/lifecycle.go, internal/mcpserver/knowledge.go, tests, docs/tools.md. ROLLBACK: revert.

## ADR-01KYN001T6E2SVBX8ZJ3FGXEPJ escalate T-01KYMPN0PNEWVS330NKPSQNRDT: rescope|reject|override-once
kind: adr
state: done
created: 2026-07-28
parent: T-01KYMPN0PNEWVS330NKPSQNRDT
decision: rescope
consequences: Rescope rather than override-once: the rounds were not thrash, they were three independent validation rounds each finding real, reproduced defects, and the task genuinely outgrew its brief. It began as make knowledge apply mint an ADR per conflict and is now make conflict curation lossless end to end, which reached internal/lifecycle (tombstone retention, gist lines), internal/knowledge (entry identity) and coordination (mint under lock). override-once would have spent a bypass while leaving that mismatch on the record, and reject would abandon a change whose core is validated and whose remaining four findings are bounded fixes to code already written.
status: accepted

T-01KYMPN0PNEWV exhausted its feedback rounds (3). Resolve via decide op=answer id=ADR-01KYN001T6E2S choose=rescope|reject|override-once.
choice: rescope
