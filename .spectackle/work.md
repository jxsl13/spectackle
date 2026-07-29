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
state: approved
created: 2026-07-28
refs: R-01KYNA6NJ3F109VTE35QYRM64Q, ADR-01KYNA70PQFTBSAP0QHYXMTVGT
grilled: 2026-07-29 open=0
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

## R-01KYNA6NJ3F109VTE35QYRM64Q gap hunt: where else does a lifecycle boundary compress a record's substance away
kind: research
state: done
created: 2026-07-28
targets: internal/lifecycle, internal/item, internal/journal, internal/replay

QUESTION. LC-001 was written after the same defect class was found twice (research tombstones dropped 268 findings' citations; adr tombstones erased both sides of every curated decision). Both were invisible until something archived. Where else does the same class hide?

METHOD. An independent agent, given only the class definition and no list of suspects, drove three throwaway git-init repos end to end - draft, move, grill, escalate, decide, reject, revoke, archive, compact, worktree submit - planting a unique marker string in each field under test and then grepping the ENTIRE .spectackle tree for that marker afterward. A field counts as lost only when no route recovers it: not get, not find at any scope, not a raw journal grep. Recoverable-but-awkward was recorded separately from lost. The structural comparison behind it was journal.Event's field set against item.Item's.

RESULT: seven findings, all at one boundary - the moment a record LEAVES work.md - carried into P-01KYN5YCXGENM. The headline is that the correspondence between item.Item and journal.Event was grown per-need and now disagrees in BOTH directions, with no test asserting it: reject preserves Targets/Parent/Rules that archive discards, while archive preserves Refs that reject discards. So the FAILURE path is more careful with structural data than the SUCCESS path. One finding is corruption rather than loss: no event carries Created, so revoke lets item.Upsert's default-to-now stamp a fresh date over the real one, silently and indistinguishably from a true value.

NEGATIVE SPACE - checked and found CLEAN, recorded because it bounds the next hunt and stops it re-treading:
- Direct archive of research and adr: the LC-001 retention holds on the path it was built for.
- EvReject's Body capture is unconditional for every kind, unlike archive's RetainsBody gate, so a rejected-then-revoked proposal/task/bug always gets its body back.
- Targets, Parent and Rules round-trip correctly through reject then revoke.
- Compaction's keep-list does protect reject/archive/compact/escalate/decide/bench forever; EvReview/EvValidate keep Pass/Hash forever and strip only Keys/Wv once terminal. Verified by reading the fold path rather than by a live grill-then-compact cycle - confidence is read-only, flagged as such.
- The worktree-to-main journal replay is verbatim and lossless including Eid; finding G6's loss is strictly in the separate simplified intentLine used for spec.md, not in event replay.
- item.LoadWork and writeWork round-trip every Item field faithfully, including hand-set Goal and Rules, for as long as the record stays IN work.md. Every loss found is at the leaving, never before it.

OFF-CLASS, found in passing and NOT part of P-01KYN5YCXGENM - transactional-boundary bugs rather than compression: a git-flow-gate-failed archive that is compensated back to done does not restore the child items the same call already folded away, and does not roll back its spec.AppendIntent, leaving a permanent duplicate intent line and a child reachable only as a tombstone. Triage separately.

CONSUMED BY: P-01KYN5YCXGENM and its child tasks. The reusable learning is the method, not the list: plant a marker, cross the boundary, grep the whole tree, and treat recoverable-only-by-raw-grep as a finding rather than a pass.

## ADR-01KYNA70PQFTBSAP0QHYXMTVGT Created has no journal channel, so revoking a rejected record lets Upsert stamp today over the real date. Carry Created in the event, or derive it from the record ID?
kind: adr
state: done
created: 2026-07-28
context: No event type has a Created field, so lastReject reconstructs an item without one and item.Upsert defaults it to time.Now(). The corruption is silent and the wrong value is indistinguishable from a real one. Record IDs are UUIDv7 and already encode mint time; ids.ParseRecordID reads it. Legacy sequential IDs (P-0007) do not.
decision: derive from the record ID (UUIDv7 mint time, via ids.ParseRecordID)
consequences: Hybrid, chosen by the maintainer. Derive Created from the record IDs UUIDv7 mint time; write it onto the reject/archive event ONLY for legacy sequential IDs (P-0007), which carry no timestamp and which this codebase commits to parsing for as long as the program exists. Rejected: carrying it unconditionally, because it duplicates a fact a modern ID already asserts and the two can then disagree, and it does nothing for records already archived without it. Rejected: deriving only, because it leaves legacy records with no date at all. The hybrid pays bytes for the legacy minority, cannot disagree with a modern ID, and repairs already-archived modern records retroactively with no migration. The invariant that matters: revoke must never stamp time.Now() over a real date again.
status: accepted

kind: radio
option: derive from the record ID (UUIDv7 mint time, via ids.ParseRecordID)
option: carry Created on the reject and archive events
choice: derive from the record ID (UUIDv7 mint time, via ids.ParseRecordID)

## B-01KYPC11VKF0QBF0HCPY3QCRJE Goal and Rules are parse-only: three gate paths read a field no tool can set
kind: bug
state: draft
created: 2026-07-29
refs: R-01KYNA6NJ3F109VTE35QYRM64Q
targets: internal/item, internal/mcpserver

VERIFIED, not inferred. Across the whole tree, it.Goal is assigned at exactly one site (internal/item/item.go:244) and it.Rules at exactly one (internal/item/item.go:248) - both inside LoadWork, i.e. the parser reading back what is already on disk. No tool writes either. draft and the draft-revise path set only Title/Body/Targets/Refs.

CONSEQUENCE. Goal is READ by three gate paths - the work-submit gate, the swarm gate and the validate path - so a documented gate can never fire, because the only way to populate the field is hand-editing work.md, which the server's own instructions forbid outright (NEVER edit these files yourself). Rules is carried faithfully through reject and archive events and is rendered, but likewise nothing can set it; rule op=add binds a rule to a DIR and to node anchors, not to an item.Rules list.

THIS IS NOT THE BOUNDARY-LOSS CLASS. P-01KYN5YCXGENM is about substance destroyed when a record leaves work.md; this is a field that can never hold substance in the first place. Filed separately so neither obscures the other.

THE DECISION THIS NEEDS, before any code. Two coherent answers and they lead to opposite diffs:
(a) Goal is a real feature that was never wired - give draft/revise a goal argument, validate it as a shell command, and the three gates start working. Then decide who may set it (author? orchestrator only?) and whether a goal is inherited from parent to child.
(b) Goal is a vestige - the gates that read it are dead code, and the honest fix is to delete the field and the three branches, shrinking the machine rather than growing it. Same question for Rules: if rule anchoring by dir plus applies is the real binding, item.Rules is a second, weaker mechanism that should go.

Do not implement either until that is decided; the wrong choice adds surface to a tool whose stated constraint is minimal surface. Evidence to gather first: whether ANY record in this repository's history ever carried a non-empty goal or rules line (search the journal's reject/archive events, which do carry Rls) - if the answer is never, in a repository that has dogfooded itself for its entire life, that is strong evidence for (b).

VERIFY once decided: for (a), a test that sets a goal through the tool surface and proves each of the three gates observes it; for (b), that the field and every branch reading it are gone and the suite is green.

## T-01KYPC2NM8EAXSG7G31FCFAMQ7 item-to-event field correspondence, made mechanical by a round-trip property test
kind: task
state: active
created: 2026-07-29
parent: P-01KYN5YCXGENMRNK00CQTPJM1P
refs: R-01KYNA6NJ3F109VTE35QYRM64Q, ADR-01KYNA70PQFTBSAP0QHYXMTVGT
grilled: 2026-07-29 open=0
targets: internal/journal, internal/lifecycle

Closes G1-G5 of P-01KYN5YCXGENM. Root cause: journal.Event's field set was grown per-need and now disagrees with item.Item's in BOTH directions, with nothing asserting the correspondence. Reject preserves Targets/Parent/Rules that archive discards; archive preserves Refs that reject discards. The FAILURE path is more careful with structural data than the SUCCESS path.

TODAY, verified in internal/lifecycle/lifecycle.go. The EvReject event (two construction sites, the to==StateRejected arm and the reject-with-snapshot arm) writes Ev/ID/K/Ti/Sum/Note/Dir/Body/Tg/Par/Rls/Rnd/Gr/Nd/Ov. It omits Refs and all four ADR fields (Context/Decision/Consequences/Status). The EvArchive event writes Ev/ID/K/Ti/Sum/Rls/Dir/Refs plus the retainedBody. It omits Par and Tg entirely - journal.Event has Par/Tg fields, archive simply does not set them - and omits Rounds/Grilled/Needs/Override. Neither carries Created.

CHANGE.
1. internal/journal/journal.go: add the channels that do not exist. The ADR fields (Context/Decision/Consequences/Status) have NO Event field at all today; adrOutcome smuggles them into Body as text for archive only, which is fine for a human tombstone read but is not a field a reader can address. Add them as first-class optional fields so reject can snapshot them and revoke can restore them structurally. Add Created per (3). Keep every json tag short - these ride every event and the journal is a token surface.
2. internal/lifecycle/lifecycle.go: populate the full set at BOTH boundaries. Reject snapshots everything needed to reconstruct the item (it is explicitly a revocation snapshot, so completeness is its whole job). Archive carries the structural fields it currently drops (Par, Tg) so a tombstone can answer what a record belonged to and touched. Keep archive's existing retainedBody behavior unchanged - LC-001 and its tests own that and this task must not perturb it.
3. Created, per ADR-01KYNA70PQFTB (hybrid): derive from the record ID's UUIDv7 mint time via ids.ParseRecordID and RecordID.Time(); write Created onto the event ONLY when the ID is legacy sequential (P-0007 form) and therefore carries no timestamp. lastReject and Tombstone must set Created from the derived-or-carried value so item.Upsert's default-to-now can never fire on a revoke. That default stamping a fresh date over a real one, silently and indistinguishably, is the one CORRUPTION in this proposal rather than a loss - fix it first and test it hardest.
4. The mechanism, not just the fix: a ROUND-TRIP PROPERTY TEST over every item.Item field. Build an item with every field set to a distinctive value, cross each boundary that serializes one (reject then revoke; archive then Tombstone), and assert field-for-field what MUST survive and what is deliberately dropped. The drop list is an explicit, named allow-list in the test - so adding a field to item.Item fails the test until someone states which side of the line it is on. That is what stops this class recurring; the individual field fixes are secondary.

SCOPE BOUNDARY. Do NOT touch internal/replay (G6 is T2's, sibling task, disjoint file set) and do NOT touch the Goal/Rules write path (B-01KYPC11VKF0Q, deliberately not in this proposal).

BENCHMARK, per the standing mandate. Record a boundary-fidelity benchmark before and after, the same shape as M-01KYMVV3J0E1Y: build one record of each kind with every field populated, cross each boundary, and count fields recoverable afterward via get and find - never by raw journal grep, which is not a route a caller has. Metrics: fields_survived (up), fields_corrupted (down). Expect the before run to show a nonzero fields_corrupted from the Created stamp; that number reaching zero is the headline.

TESTS: reject then revoke restores every ADR field and Refs; revoke preserves a hand-set Created for a modern ID and for a legacy one; archive tombstone answers Parent and Targets; the round-trip property test with its explicit drop allow-list; and the existing LC-001 retention tests still pass untouched.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l . empty. ROLLBACK: revert.

## T-01KYPC2PQ0ENKV22TW5KMKFE5G replay writes the same intent line archive does, so a worktree-archived record keeps its note on main
kind: task
state: draft
created: 2026-07-29
parent: P-01KYN5YCXGENMRNK00CQTPJM1P
refs: R-01KYNA6NJ3F109VTE35QYRM64Q
grilled: 2026-07-29 open=0
targets: internal/replay

Closes G6 of P-01KYN5YCXGENM. Narrow, one function, disjoint from its sibling T1.

TODAY. internal/replay/replay.go's intentLine is \"- \" + e.ID + \" \" + e.Ti - ID and title only. lifecycle.archive's own line is the same prefix plus \": \" + firstOf(note, gistLine(it)). Git never merges .spectackle text (wt.go's codeOnly pathspec excludes it unconditionally) and replay.Run is the ONLY writer of main's .spectackle state, so main's spec.md gets the stripped line for EVERY item ever archived through the worktree flow - which is the documented primary workflow. Verified end to end: a task archived in a worktree with a distinctive note showed the full line in the worktree's own spec.md and the bare line on main after submit.

NOT DATA LOSS, AND SAY SO IN THE FIX. The note survives in the replayed journal event's Sum - replay itself is verbatim and lossless including Eid. What is incomplete is the one artifact whose stated purpose is to be the permanent human-readable trace. Frame the change as parity, not recovery.

CHANGE. intentLine must compose the same line archive does, from the event rather than the item: the note rides EvArchive as Note, and the gist equivalent is available from Sum (archive writes Sum = summary(it) + optional note suffix) or, for kinds that retain one, from Body. Prefer reconstructing from the SAME inputs archive used rather than re-deriving a second formatting rule - two functions that must agree about a permanent artifact is exactly the shape that produced this bug. If the cleanest fix is to export a single shared composer from internal/lifecycle and have both call it, do that; a shared composer is the durable answer and a duplicated format string is not.

WATCH: replay is idempotent by Eid and runs on merge; the change must not make a replayed line differ from the worktree's own, or a later reconciliation will see spurious drift. Assert byte equality between the two in the test rather than asserting the note is merely present.

TESTS: archive an item in a worktree with a note, submit, and assert main's spec.md intent line is BYTE-IDENTICAL to the worktree's; the same for an item archived with no note (the no-note form must also match); and idempotence - replaying twice adds one line, not two.

VERIFY: go build ./... && go test ./internal/replay/ ./internal/lifecycle/ ./internal/mcpserver/ -count=1 && gofmt -l . empty. ROLLBACK: revert.

## B-01KYPC60DWEZ0S0CN1RFTEPGQH the done edge pushes a branch that was never created when a record goes straight to done without passing through active
kind: bug
state: draft
created: 2026-07-29
targets: internal/mcpserver

REPRODUCED just now, in this repository. An R-item drafted and then moved straight to done - a legitimate path for a research record, which needs no implementation branch - hit:

! GIT E R-01KYNA6NJ3F109VTE35QYRM64Q push: git push -u origin spectackle/R-01KYNA6NJ3F10: exit status 1: error: src refspec spectackle/R-01KYNA6NJ3F10 does not match any

OBSERVED vs EXPECTED. The state transition itself succeeded - the item reads done afterward - so this is a noisy, misleading failure rather than a broken edge: it reports a GIT E against an item that is now in the state the caller asked for. Expected: an item that never entered active has no branch by construction (work op=start is what creates one), so the done edge should not attempt a push at all, and certainly should not report an error for the absence of something it never made. git branch -a confirms no such ref exists locally either.

WHY IT MATTERS beyond the noise. A GIT E line trains the reader that something needs fixing; here nothing does. Worse, it is emitted on the one record kind whose normal lifecycle SKIPS active - research is drafted, read, and closed - so the false alarm fires precisely on the path that is working correctly. It also costs tokens on every such transition and, per RENDER-PARITY, an error line that means nothing is the most expensive kind.

ISOLATED CAUSE, likely. The done edge derives a branch name from the item ID unconditionally and pushes it, rather than first asking whether this item ever had a worktree or branch. The coord worktree ledger and the swarm state already know the answer; the item's own history (did it ever reach active) answers it too.

DIRECTION. Gate the push on the item having actually had a branch. Prefer asking the ledger over inferring from state, since a branch can outlive a state transition. When there is no branch, say nothing at all - a record that never needed one is not an event worth a line.

TESTS: draft an item, move it straight to done, and assert the render carries no GIT line and no error; the same for draft to archived; and the existing active-then-done path still pushes and still reports.
