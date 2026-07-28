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
state: done
created: 2026-07-28
parent: P-01KYMCKE8DEW7BZ3FNCMJTNSG2
rounds: 2
grilled: 2026-07-28 open=0
targets: internal/mcpserver, internal/lifecycle, internal/knowledge, docs

Implements ADR-01KYMKEG7YE2P (decide-integration) for P-01KYMCKE8DEW7. Round 2 after an independent validator reproduced three defects in round 1; the round-1 brief was itself wrong on point (5) and that mistake shipped.

TODAY (round 1, on branch): knowledgeApply gained in.Paths, merged them, and minted one ADR per knowledge.Conflict. Three holes, each empirically reproduced in throwaway repos:

D1 CRITICAL, internal/lifecycle/lifecycle.go archive(): the tombstone retains a body only for it.Kind=="research". Every other kind is compressed to summary(it) = Kind+Title+" - "+firstLine(Body). mintConflictDecision always writes "kind: radio" as the body's first line, so every conflict ADR archives to the identical contentless string "adr <question> - kind: radio". Both competing decisions then vanish from get, from work.md and from find at every scope; the projects own compact housekeeping reaches this with no user intent. The loser-preservation promise this task was written to keep is therefore false today. FIX: the retention carve-out is not about research, it is about kinds whose body IS the outcome because no delta of theirs merges into spec.md - that is research AND adr. Extend it to adr, and carry the ADR's structured outcome with it: Decision, Status, Context and Consequences live in item.Item fields that journal.Event has NO channel for, so retaining Body alone still loses which side won. Render those fields into the retained body. Do the same in the folded-done-children arm of archive(), which has its own duplicate of the research check. summary() must prefer an adr's Decision over firstLine(Body) so the spec.md intent line archive() appends is substantive rather than "kind: radio". get must print an adr tombstone's retained body exactly as it already prints a research one (internal/mcpserver/tools.go, the Tombstone branch).

D2, identity: conflict minting is not idempotent, and the root cause is one layer below the conflict loop. conflicts comes straight from knowledge.Merge over the incoming artifacts with no reference to what the target workspace already holds, so a re-apply mints a fresh duplicate ADR - both before answering (two unlinked open ADRs asking the identical question) and after (a second ADR for a question already accepted). FIX, part one, internal/knowledge/artifact.go: make entry identity content-derived in ONE place. knowledge.Parse took the section heading's key verbatim off the wire, while NewEntry refuses a caller-supplied key outright (its own doc: \"a caller-chosen key would break dedup\") and Extract derives it from content. So identity meant whatever the producing repository happened to write, and every downstream identity check was only as sound as that: FoldInto re-added the same rule on every apply of a hand-authored or foreign-keyed artifact - making the documented additive idempotence false - Merge never bucketed two repositories answering one question, and the duplicate-decision check below could not work at all. Extract a contentKey(Entry) helper (NormHash of Text/Question/Prose by kind), route Extract and NewEntry through it, and have Parse recompute rather than trust. Keep the wire key only when there is no identifying payload to hash, so structurally incomplete entries do not all collapse onto one empty identity. A legitimate artifact round-trips unchanged, because its keys already equal contentKey; only fabricated ones are normalized, which is the point. FIX, part two, knowledge.go: apply FoldInto's own rule to conflicts. knowledge.Extract emits a KindADR entry for every adr item with Key=NormHash(question) regardless of state, so the workspace's own artifact already answers "do we hold an opinion on this question" for the minted-open case, the answered case and the case where this repo decided it independently. Skip a conflict whose (KindADR, Key) identity current already carries and say so compactly; mint only genuinely new ones. This is the same local-wins precedence every non-conflicting entry already gets.

D3, knowledge.go: the safety net is bypassed by the legacy single-artifact route. The Merge branch is guarded on len(in.Paths)>0, and round-1's brief mandated a len(artifacts)>1 guard - both are wrong for the same reason: knowledge.Merge buckets entries across AND within artifacts, and knowledge export of a workspace holding two same-question ADRs emits ONE artifact carrying both. Applying that export via path= skips detection entirely and silently drops a side. Validated with no hand-editing: two decide op=ask calls with the same question, answered differently, exported, applied - one decision landed, the other gone from work.md, journal and find. FIX: delete the guard. Gather artifacts uniformly from path/paths/body and always Merge. This is backward compatible by construction: Parse already sorts entries with sortEntries, the same comparator Merge applies, and groupBySubstance only unions provenance for entries FoldInto would dedupe anyway - so a single conflict-free artifact produces an identical delta and an identical render.

D4 docs/tools.md: the knowledge schema block now carries two "paths" keys, the new one and the untouched merge-only one. Merge into one description.

D5 knowledge.go mintConflictDecision: a side whose Decision and Text are both empty renders as a bare "option: <src>:" and decide op=answer accepts that blank string as the winner. Give an empty side an explicit placeholder so no option is choosable-but-blank.

TESTS: (a) archiving a conflict ADR keeps BOTH sides and the chosen Decision readable through get and find scope=history - the direct regression for D1, the defect that made this feature a no-op; (b) re-applying the same conflicting artifacts mints no second ADR, before and after answering, and the assertion must be exact equality of the minted-ID set, not the round-1 "no more than one new ADR" bound that passed while the bug was live; (c-key) Parse recomputes a fabricated key to the content key, and two unhashable entries keep distinct wire keys; (c) a SINGLE artifact carrying two same-question ADRs opens a conflict decision rather than dropping a side (D3); (d) single conflict-free artifact via path= and body= - trailer and lines unchanged; (e) an empty-decision side is never a blank option. Keep the round-1 tests.

VERIFY: go build ./... && go test ./... -count=1 && gofmt -l . empty.

SCOPE: internal/mcpserver/knowledge.go, internal/mcpserver/tools.go (adr tombstone render), internal/lifecycle/lifecycle.go, internal/knowledge/{artifact,extract}.go, tests, docs/tools.md. ROLLBACK: revert.
