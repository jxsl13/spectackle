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

## R-01KYQ4XNAFFNYSTNRKC28BR3N3 judged friction: four independent agents drive the tricky scenario from tool output alone
kind: research
state: draft
created: 2026-07-29
targets: internal/mcpserver

METHOD. Four fresh agents, each given only bench -agent-prep -scenario tricky's 15-line brief and the metered CLI, with no documentation and no access to the source. The brief states outright that the tool outputs themselves are the only guide. Goals span the hard transitions: mint an EARS rule with a slot-complete response, drive a task through repeated done/active reopens until the server refuses and escalates, resolve the minted decision by rescope, and finish with a clean check. 79 tool calls across the four runs.

HEADLINE. 4/4 reached DONE, so the surface is VALID - an agent can complete the scenario from the outputs alone. The cost is where it fails: 18, 26, and two comparable runs, against a floor nearer 12. Friction is concentrated in discoverability, and one item is a correctness defect rather than wording.

RANKED, by judges affected times calls wasted:
1. state emits no next step and no shape hint (4 judges, 8 calls). Every judge is told to start at state; all four then discovered argument shapes by firing empty objects at each tool. state renders version, rules, graph, swarm counters and nothing actionable.
2. rule with empty args prints a flat 14-field union with only op starred (4 judges, 4 calls), reading as everything-else-optional. The correct op-conditional shape ALREADY EXISTS and is emitted only after the first failure.
3. The rounds refusal names the decide tool and the three choices but not the callable JSON (4 judges, 4 calls), while sibling refusals do hand back a shape line - the inconsistency is what costs the call.
4. CORRECTNESS, not wording: the rounds refusal returns exit 0 and prints an i line byte-identical in shape to a successful move, for a state the caller never requested (3 judges, 5 calls). Judges read it as moved-plus-warning; one then issued five further move calls against a blocked item. Both sites use text() where they mean refuse().
5. check returns the bare string ok (4 judges, 3 calls) - indistinguishable from a no-op on a verification command, so judges spent a confirming state call.
6. draft is the only tool whose error carries no shape line, and it never enumerates the legal kinds (4 judges, 3 calls).
7. decide op=answer never reports what happened to the blocked item it resolved (3 judges, 3 calls).
Lower: check's shape is unreachable because empty args are valid; the same record renders at two truncation lengths one line apart, inviting a paste error; move's shape omits the to enum and the rounds budget is invisible until spent.

SILENT DEAD ENDS, the class judges called worst because nothing is learned: rule op=add accepts and discards slots irrelevant to the chosen EARS pattern; move accepts a note that never appears in any output.

THE PAYING CONSTRAINT. Additions must be funded by removals, since the metric is tokens per call. Judges identified the funding: the graph section on a workspace with zero edges, the swarm section when the only agent is this session with no leases or worktrees, and the version string's build suffix - none of which any judge used, together roughly covering the cost of a next-step line on state.

CONSUMED BY: the proposal this research anchors. The reusable method: give an agent the brief and nothing else, count calls rather than asking it whether the wording was clear, and rank by judges-affected times calls-wasted so a single judge's stumble does not outrank a systematic one.

## P-01KYQ4YK7MEA3BP26HSQ7CWZ4R the tool surface is valid but under-directs: a refusal that looks like success, and shapes withheld until after a failure
kind: proposal
state: draft
created: 2026-07-29
refs: R-01KYQ4XNAFFNYSTNRKC28BR3N3
grilled: 2026-07-29 open=0
targets: internal/mcpserver, internal/lifecycle

Anchored on R-01KYQ4XNAFFNY: four independent agents drove the tricky scenario from tool output alone, 79 calls. All four finished, so the surface is VALID; what it is not is economical or trustworthy, and one defect is correctness rather than wording.

THE CORRECTNESS DEFECT. A move that is REFUSED returns exit 0 and prints an i line byte-identical in shape to a successful move, naming a state the caller never asked for (internal/mcpserver/tools.go, both rounds-exhausted sites use text() where they mean refuse()). Three judges read it as moved-plus-warning; one then issued five further move calls against an item that was never in active. A caller that trusts the primary line is driven into a loop the severity marker on the NEXT line silently contradicts. This is the one item that can produce an invalid result rather than merely an expensive one.

THE ECONOMIC DEFECT, one shape. Every judge is instructed to start at state; state renders counters and no next step, so all four discovered argument shapes by firing empty objects at each tool - 8 calls spent on discovery the first call could have carried. The same pattern repeats: rule prints a flat 14-field union with only op starred, and emits the correct op-conditional shape only AFTER the first attempt fails; the rounds refusal names the decide tool and its three choices but withholds the callable JSON that sibling refusals do provide; draft alone carries no shape line at all and never enumerates its kinds. In every case the right text already exists somewhere in the codebase and is withheld until the caller has paid a round trip for it.

THE FUNDING CONSTRAINT, which shapes the work. The metric is tokens per call, so additions must be paid for by removals, not appended. Judges identified unused output on the hottest path: the graph section on a workspace with no edges, the swarm section when the only agent is this session with no leases and no worktrees, and the build suffix on the version string. None was used by any judge in 79 calls. A next-step line on state is roughly covered by those three.

SCOPE, two tasks by disjoint concern. One: stop lying about outcomes - refusals refuse, and a verification command reports what it verified. Two: put the shape where the caller already is, funded by deleting output nobody reads. The second is meaningless without a before/after judged measurement, so it carries one.

DELIBERATELY NOT IN SCOPE. The silent dead ends judges flagged - rule op=add discarding slots irrelevant to the chosen pattern, and move accepting a note that never surfaces - are real and are the worst class (the caller learns nothing), but each needs its own decision about whether the input should be refused or honored, and neither cost a judge a call. File them; do not fold them in here.

## T-01KYQ503AGE6TV1NWY3EAVZSA6 a refusal must refuse: the rounds-exhausted move stops reporting success, and check reports what it checked
kind: task
state: draft
created: 2026-07-29
parent: P-01KYQ4YK7MEA3BP26HSQ7CWZ4R
refs: R-01KYQ4XNAFFNYSTNRKC28BR3N3
grilled: 2026-07-29 open=1
targets: internal/mcpserver, internal/lifecycle

Closes the correctness half of P-01KYQ4YK7MEA3. Three judges misread the same output; one acted on the misreading.

D1 CORRECTNESS, internal/mcpserver/tools.go, the two rounds-exhausted returns (the coord-emit-failed arm and the normal arm). Both call text(), so the CLI exits 0, and both print i <id> <kind> blocked <dir> <title> - the same shape the item record renderer emits for a SUCCESSFUL move, differing only in the state word. The requested transition did not happen; the item was forced to blocked instead. FIX: return refuse() at both sites so the exit code matches the outcome, and drop the i line - a record line for a state the caller did not request is the thing being misread. Say what did not happen before saying what is now true: move to <requested> REFUSED - rounds exhausted, item is now blocked. Then give the resolution as a CALLABLE object, not a prose list of choices, because the sibling refusal for rule already hands back a shape line and the inconsistency is itself what cost a call: decide {\"op\":\"answer\",\"id\":\"<adr>\",\"choose\":\"rescope|reject|override-once\"}, with the outcome of each choice named (rescope to draft, reject to rejected, override-once to active) since that is what the caller is actually deciding between. Dropping the i line reclaims most of the bytes the callable object costs, and this is a cold path that fires once per escalation.

D2, same file, the check tool's zero-findings return. It returns the bare string ok. Goal-shaped questions are phrased in terms of findings and severities, and a two-character answer on a VERIFICATION command is indistinguishable from a no-op stub - three of four judges spent a confirming state call, which costs several hundred bytes, to believe it. FIX: report what was checked and what was found, from counts the same scan already has: ok check <path> 0 findings (E=0 W=0) - <n> rules <n> dirs <n> items. Net positive despite being longer, because it deletes the corroborating call it currently provokes.

D3, internal/lifecycle, the escalation ADR title. It is built from the FULL item ID while every display path renders through the short form, so state shows the same record at two different truncation lengths one line apart - inviting a caller to paste a long ID where a short one is expected. FIX: build the title from the short form, which is already used one line above in the same function. Titles minted before this keep the long form; nothing resolves against titles, so no migration.

TESTS: a rounds-exhausted move returns a refusal (non-zero) whose text names the refused destination and carries a callable decide object, and does NOT contain an i record line; a clean check names its counts and severities; an escalation ADR title renders at the same length as the record it names. Assert the ABSENCE of the i line explicitly - that is the defect, and a test that only checks the new text would pass with the old line still there.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l . empty. Then re-run a judge: bench -agent-prep DIR -scenario tricky, drive it, and confirm the rounds refusal is not misread. SCOPE: the two rounds returns, the check zero-findings return, the escalation title. Do NOT touch state's sections or any shape line - that is the sibling task. ROLLBACK: revert.

## T-01KYQ5047CE5MSBF7KTM3BGKVQ put the shape where the caller already is, funded by deleting output no judge read
kind: task
state: draft
created: 2026-07-29
parent: P-01KYQ4YK7MEA3BP26HSQ7CWZ4R
refs: R-01KYQ4XNAFFNYSTNRKC28BR3N3
grilled: 2026-07-29 open=0
targets: internal/mcpserver

Closes the economy half of P-01KYQ4YK7MEA3. 8 of 79 judged calls were spent rediscovering argument shapes that the codebase already knows.

D1, internal/mcpserver/state.go. Every judge is told to start at state and none learned anything actionable from it. Add a final section naming the tools and the discovery rule in one line - that empty arguments return a shape. This is the hottest path on the surface, so it must be PAID FOR, not appended. Fund it in the same function: suppress the graph section when there are no edges and no typed-pass finding, suppress the swarm section when the only agent row is this session with no leases and no worktrees, and emit the version without its build suffix. No judge used any of the three in 79 calls; together they roughly cover the addition. Measure the net, and if it is positive, say so in the archive note rather than claiming a win.

D2, the rule tool. Empty arguments render a flat union of fourteen properties with only op starred, which reads as everything-else-optional; a judge then sent a plausible add and was bounced with a DIFFERENT and correct op-conditional shape. The good text already exists in the failure path. FIX: emit the op-conditional form the first time, so the shape a caller sees is the shape they can call.

D3, the draft tool. It is the only tool whose refusal carries no shape line at all, and it never enumerates the legal kinds - an unenumerated required string is the worst discoverability hole on the surface, because a wrong guess is a silent semantic error rather than a refusal. FIX: emit the same shape line every sibling emits, with the kind enum spelled out.

D4, decide op=answer. It reports the decision but never what happened to the item the decision unblocked, so a judge must call get to learn whether the rescope landed. FIX: name the resulting item state in the same line.

MEASUREMENT IS PART OF THIS TASK, not optional. Before: four judges already ran this scenario (R-01KYQ4XNAFFNY) at 79 calls. After: prep four fresh tricky workspaces, run four fresh independent judges with the same prompt, and record calls and metered bytes as a benchmark version against the before run. The claim to test is fewer calls at no more bytes per call. If calls do not fall, the additions are not earning their place and should be reverted rather than kept for plausibility.

TESTS: state renders the next-step line; state omits graph/swarm on a solo workspace with no edges and no leases, and still renders them when either is non-trivial; rule with empty arguments returns the op-conditional shape; draft with empty arguments returns a shape line naming every kind; decide op=answer names the item's resulting state.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l . empty, plus the four-judge after-run. SCOPE: state.go's sections, the rule/draft shape emission, decide's answer line. Do NOT touch the rounds refusal or check - sibling task. ROLLBACK: revert.
