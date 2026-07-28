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

## B-01KYN3E973F20VH7DHPE1YSSD7 a newline in an ADR header field silently swallows every field after it into the body
kind: bug
state: draft
created: 2026-07-28
targets: internal/item

internal/item/item.go LoadWork parses the machine header as a run of contiguous key: value lines and breaks at the first line without a ": " separator. A field VALUE containing a newline therefore ends the header early: its continuation line has no separator, the loop breaks, and every header field written after it becomes part of Body instead of a struct field. Silent - no error, no warning.

REPRODUCTION (found by an independent validator during T-01KYMPN0PNEWV, confirmed pre-existing: git diff origin/main...HEAD -- internal/item/ is empty). knowledge export entries=[{kind: adr, context: "Line one.\nLine two.", decision: go-with-A, status: accepted, options: [...]}] then knowledge apply. get shows decision and status swallowed into the body text; the reloaded items .Decision and .Status are empty strings.

IMPACT is not cosmetic. Every consumer that reads those fields sees them as unset on a record that plainly has them: the archive tombstone retains an empty decision (so an archived multi-paragraph ADR loses which option won - the same loss class LC-001 was written to close, arriving through a different door), knowledge.Extract exports an ADR with no decision, and knowledge apply then reports a spurious divergence between two repositories that actually agree - the validator observed x adr ... ours="" theirs="keep as is" for identical content, caused purely by the local copy being corrupted on reload. Multi-paragraph context and consequences are the NORMAL shape for a real ADR, so this is reachable by ordinary use, not by adversarial input.

DIRECTION, not a decision - the fix needs the design context behind the work.md format. Either the header parser learns continuation lines (indented, or explicitly terminated), or the writer escapes newlines on the way out and unescapes on the way in, or the writer refuses a value it cannot round-trip rather than writing one that silently truncates. Whichever is chosen, the round trip needs a property test over values containing newlines, leading/trailing whitespace and separator characters - the existing tests only exercise single-line values, which is why this survived.

VERIFY: a test that writes every ADR field with an embedded newline, reloads, and asserts field-for-field equality.

## P-01KYN5YCXGENMRNK00CQTPJM1P the leave-work.md boundary is lossy: seven fields are dropped or corrupted when a record archives or is rejected
kind: proposal
state: draft
created: 2026-07-28
targets: internal/lifecycle, internal/item, internal/journal, internal/replay

An independent gap hunt, run after LC-001 was written, probed for more instances of the class that rule records: a record's substance compressed at a lifecycle boundary, destroying the only copy. It found seven, all at the same boundary - the moment a record LEAVES work.md - and all confirmed empirically in throwaway repos by planting a marker string and grepping the whole .spectackle tree for it afterward. item.LoadWork/writeWork round-trip every field faithfully while a record stays in work.md, so nothing here is a parse bug; the loss is entirely in what the journal event carries.

G1 REJECT drops an ADR's Context, Decision, Consequences and Status entirely. Both EvReject construction sites build from Body/Tg/Par/Rls/Rnd/Gr/Nd/Ov only. archive() captures exactly these four fields via adrOutcome (that was the LC-001 fix); reject never got the same treatment. Reject then revoke a decided ADR and the fields are gone from work.md, from get and from the raw journal - unrecoverable. Reachable by the single most obvious revisit-a-decision workflow the ADR feature has.

G2 REJECT drops Refs. journal.Event.Refs is documented \"archive/reject: item refs\" and archive does set it; neither reject site does. A two-line omission against the field's own contract.

G3 REJECT then revoke CORRUPTS Created. No Created channel exists on any event, so lastReject leaves it empty and item.Upsert's default-to-now stamps a fresh, wrong date over the real one - silently, and indistinguishably from a real value. Corruption is worse than absence: archive leaves it blank, which at least reads as unknown.

G4 ARCHIVE drops Parent and Targets. EvArchive has no Par/Tg fields at all, though EvReject does. So the FAILURE path preserves structural data the SUCCESS path discards - the same inversion the two already-fixed bugs had.

G5 ARCHIVE drops Rounds, Grilled, Needs and Override. Partially recoverable via EvEscalate/EvDecide, but only for items that escalated. An item that reopened once or twice below the escalate threshold loses Rounds at the next compaction; Grilled is worse - EvGrill is in compaction's unconditional fold bucket and archive never captures it, so a grill verdict is gone the instant its item archives.

G6 WORKTREE SUBMIT truncates the spec.md intent line. replay.intentLine is \"- \" + ID + \" \" + Title, with no note and no gist, while lifecycle.archive appends \": \" + firstOf(note, gistLine). Git never merges .spectackle text and replay.Run is main's only writer, so EVERY item archived through the swarm/worktree flow - the documented primary workflow - lands on main with a stripped intent line. The note survives in the replayed journal event, so this is degradation rather than loss, but the one artifact meant to be the permanent human-readable trace is the one that is incomplete.

G7 Goal and Rules have no write path at all. Nothing assigns either outside item.go's own parser; draft and the draft-revise handler touch only Title/Body/Targets/Refs. Goal is READ by three gate paths (gitflow, swarm, validate), so a documented gate is unreachable except by hand-editing work.md, which the server's own instructions forbid.

SHAPE. Not seven patches. The root cause is that journal.Event's field set was grown per-need and now disagrees with item.Item's in both directions, with no test asserting the correspondence. The work should decide, once and explicitly, what a tombstone owes each kind, then make the correspondence mechanical - a round-trip property test over every Item field through every boundary that serializes one (archive, reject, escalate, compact fold, worktree replay), so the next field added to Item cannot silently fall through. G3 additionally needs a decision on whether Created belongs in the event or should be derived from the record ID's own UUIDv7 timestamp, which ids.ParseRecordID can already read.

RELATED, filed separately: B-01KYN3E973F20 (a newline in a header field makes LoadWork swallow every later field into Body) is the same data-integrity area but a parser bug, before the boundary rather than at it.

ALSO FOUND, off-class, flagged for triage not for this proposal: a git-flow-gate-failed archive that is compensated back to done does not restore the child items the same call already folded away, and does not roll back its spec.AppendIntent - leaving a permanent duplicate intent line and a child reachable only as a tombstone. Transactional-boundary bugs, not compression bugs.

## B-01KYN5ZYM1FY2TBZHXC43V68TE rule applies renders a never-resolvable anchor identically to a not-yet-indexed one, and the difference only surfaces as a red CI gate after the PR leaves draft
kind: bug
state: draft
created: 2026-07-28
targets: internal/mcpserver, internal/drift

Hit while landing T-01KYMPN0PNEWV. rule op=add applies=[internal/knowledge/artifact.go] was accepted and rendered a internal/knowledge/artifact.go pending (node not indexed yet). That reads as a transient state that a reindex will clear. It is not: anchors bind GRAPH NODES, whose names are go:pkg.Symbol, and a file path is not a node name in any index state, so the anchor stays pending forever. spectackle reindex (259 files, 2861 nodes) did not change it.

WHY IT MATTERS BEYOND THE CONFUSION. The repositorys own CI self-hosting gate requires the check tool to print exactly ok. A pending anchor makes check print ok 2 anchors pending (nodes not in the graph yet), which is a truthful non-error but not the literal ok, so the build fails. Because the archive edge flips the PR out of draft BEFORE awaiting checks (gitflow.go, the pr.Draft arm), the first red signal arrives after the one draft-to-ready flip PR-DRAFT-001 exists to make single, and archive refuses with closure merge did not complete. So a wrong anchor argument, accepted silently at rule-add time, surfaces as a merge failure several steps later with nothing pointing back at the cause.

OBSERVED vs EXPECTED. Observed: identical pending render for two different conditions, and no signal until the merge gate. Expected: a not-yet-indexed anchor (a symbol that will exist) and an unresolvable one (a string that is not a node name) are different states and should not render the same. A path-shaped argument is a particularly cheap case to catch - it contains a separator and a file extension and matches no node - so the add path can say so at the moment the caller can still fix it.

DIRECTION, not a decision. Options, roughly increasing in strictness: (a) render the two states distinctly, e.g. a <rule> <anchor> unresolvable - anchors name graph nodes (go:pkg.Symbol), not paths; (b) additionally suggest the node, since find scope=code already resolves a path to the symbols declared in it; (c) refuse a path-shaped applies outright at rule op=add. Whichever is chosen, check should distinguish never-resolvable from pending in its own output too, since a permanently pending anchor is a defect while a freshly added one is not.

VERIFY: a test that adds a rule with a path-shaped applies and asserts the render names it unresolvable; a test that check separates the two classes.

## B-01KYNA4PJNF5KAH6M0640ZY7ZT ADR status superseded is assignable free text: nothing links a replacement to what it retires, and retired decisions never leave find scope=adr
kind: bug
state: draft
created: 2026-07-28
targets: internal/item, internal/mcpserver

Found by a researched comparison against edg-l/engram-mcp, then verified directly against this codebase.

TODAY. item.Item.Status is a bare string (internal/item/item.go:92); the enum proposed|accepted|superseded|deprecated exists only in a doc comment there and in a jsonschema DESCRIPTION at internal/mcpserver/tools.go:117, which is documentation, not validation - nothing rejects an arbitrary value. The only place all four values appear in executable code is a test. find scope=adr maps to kinds {adr} (internal/mcpserver/tools.go:323) with no status predicate, so a retired decision occupies result slots forever and is indistinguishable from a live one in the render. And nothing anywhere records WHICH decision replaced a retired one: supersession is an assertion an agent types into a field, with no edge, no event, and no way to ask what superseded ADR-X.

WHY IT MATTERS NOW, not hypothetically. knowledge apply mints an ADR per merge conflict and flips it to accepted on answer (T-01KYMPN0PNEWV, just landed). As repositories exchange knowledge repeatedly, decisions on the same question accumulate: the workspace ends up holding several accepted ADRs for one question, ordered by nothing, with the superseding relationship recorded nowhere. The feature that just shipped to stop conflicts from vanishing therefore has no answer to which surviving decision is current. find scope=adr degrades monotonically as a repository ages, which also makes it a token-cost regression on the hottest research path.

DIRECTION, decided by the comparison. Make superseded UNREACHABLE BY ASSIGNMENT: it becomes a consequence of minting a replacement that names its predecessor, never a value an agent writes. Concretely: (1) validate Status against the four values at the write path, refusing anything else; (2) refuse a direct transition to superseded, with the refusal naming the operation that IS allowed; (3) the replacement path writes ONE journal event carrying both IDs - compaction's keep-list already preserves decide forever, so the edge survives archival without new retention machinery; (4) find scope=adr excludes superseded by default with an opt-in to include them, and get on a superseded ADR names its replacement.

REJECTED ALTERNATIVE, and why. engram-mcp wraps insert + status flip + edge in one SQLite transaction to avoid orphaned pairs. Do not copy that: an append-only journal makes orphans impossible when both IDs ride a single event, so the transaction is machinery this design does not need. Copy the framing (superseded is a consequence), not the mechanism.

TESTS: minting a replacement retires the predecessor and both IDs land in one event; a direct status=superseded write is refused and the refusal names the allowed operation; find scope=adr returns only live decisions by default and all of them with the opt-in; get on a retired ADR names its replacement; an invalid status value is refused.

MEASURE BEFORE SHIPPING THE FILTER. On a workspace holding at least five retired ADRs, benchmark the find scope=adr output token delta with and without the default filter. If it sits inside the bench noise floor, ship the validation and the edge and skip the filter - it is then discipline rather than savings, and should be justified as such rather than as a token win.

VERIFY: go build ./... && go test ./... -count=1 && gofmt -l . empty.
