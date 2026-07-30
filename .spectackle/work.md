---
schema: v1
---

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
state: active
created: 2026-07-28
rounds: 1
grilled: 2026-07-29 open=0
targets: internal/item, internal/mcpserver

VERIFIED against the code, and the exposure is wider than first filed.

TODAY. item.Item.Status is a bare string. The enum proposed|accepted|superseded|deprecated exists in exactly two places, neither of which validates: a doc comment at internal/item/item.go and a jsonschema DESCRIPTION at internal/mcpserver/tools.go. Nothing rejects an arbitrary value. Worse than a caller typo: internal/mcpserver/knowledge.go's ADR-apply path assigns d.Status = e.Status straight from an IMPORTED artifact, so a foreign repository can inject any string into this workspace - including superseded, which is supposed to be a consequence rather than a claim. find scope=adr also has no status predicate, so a retired decision occupies result slots forever and is indistinguishable from a live one.

WHY IT MATTERS NOW, not hypothetically. knowledge apply mints an ADR per merge conflict and flips it to accepted (T-01KYMPN0PNEWV, landed). As repositories exchange knowledge repeatedly, decisions on one question accumulate; nothing records which replaced which, so the feature built to stop conflicts vanishing has no answer to which surviving decision is current. find scope=adr therefore degrades monotonically as a repository ages, on the hottest research path.

SCOPE, narrowed deliberately after verification. This item now covers ONLY the half that is unambiguous and cheap: (1) one validator, item.ValidStatus, accepting the four values and empty; (2) every write path that takes a status from outside the server validates through it - the imported-artifact path first, since that is untrusted input, and the tool boundary second; (3) superseded is REFUSED from any direct assignment, with the refusal naming the operation that is allowed, because a record cannot truthfully claim to be superseded without naming what replaced it.

SPLIT OUT, not done here: the supersession EDGE - minting a replacement that names its predecessor, both IDs in one journal event, and get on a retired ADR naming its replacement - is a design change large enough to deserve its own record, and it depends on this validation existing first. Likewise the find scope=adr default filter, which the earlier version of this item required be MEASURED before shipping: on a workspace with at least five retired ADRs, compare the find output token delta with and without it, and if the difference sits inside the bench noise floor then ship the validation and skip the filter, calling it discipline rather than savings.

REJECTED ALTERNATIVE. engram-mcp wraps insert plus status flip plus edge in one SQLite transaction to avoid orphaned pairs. Do not copy that: an append-only journal makes orphans impossible when both IDs ride a single event, so the transaction is machinery this design does not need. Copy the framing - superseded is a consequence - not the mechanism.

TESTS: an invalid status is refused at the tool boundary and on the imported-artifact path; a direct status=superseded is refused with the allowed operation named; the four valid values and empty all pass; an artifact carrying a bogus status does not poison the workspace.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l . empty.

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

## P-01KYQ4YK7MEA3BP26HSQ7CWZ4R the tool surface is valid but under-directs: a refusal that looks like success, and shapes withheld until after a failure
kind: proposal
state: approved
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
state: active
created: 2026-07-29
parent: P-01KYQ4YK7MEA3BP26HSQ7CWZ4R
refs: R-01KYQ4XNAFFNYSTNRKC28BR3N3
rounds: 1
grilled: 2026-07-29 open=0
targets: internal/mcpserver, internal/lifecycle

Closes the correctness half of P-01KYQ4YK7MEA3. Three judges misread the same output; one acted on the misreading.

D1 CORRECTNESS, internal/mcpserver/tools.go, the two rounds-exhausted returns (the coord-emit-failed arm and the normal arm). Both call text(), so the CLI exits 0, and both print i <id> <kind> blocked <dir> <title> - the same shape the item record renderer emits for a SUCCESSFUL move, differing only in the state word. The requested transition did not happen; the item was forced to blocked instead. FIX: return refuse() at both sites so the exit code matches the outcome, and drop the i line - a record line for a state the caller did not request is the thing being misread. Say what did not happen before saying what is now true: move to <requested> REFUSED - rounds exhausted, item is now blocked. Then give the resolution as a CALLABLE object, not a prose list of choices, because the sibling refusal for rule already hands back a shape line and the inconsistency is itself what cost a call: decide {\"op\":\"answer\",\"id\":\"<adr>\",\"choose\":\"rescope|reject|override-once\"}, with the outcome of each choice named (rescope to draft, reject to rejected, override-once to active) since that is what the caller is actually deciding between. Dropping the i line reclaims most of the bytes the callable object costs, and this is a cold path that fires once per escalation.

D2, same file, the check tool's zero-findings return. It returns the bare string ok. Goal-shaped questions are phrased in terms of findings and severities, and a two-character answer on a VERIFICATION command is indistinguishable from a no-op stub - three of four judges spent a confirming state call, which costs several hundred bytes, to believe it. FIX: report what was checked and what was found, from counts the same scan already has: ok check <path> 0 findings (E=0 W=0) - <n> rules <n> dirs <n> items. Net positive despite being longer, because it deletes the corroborating call it currently provokes.

D3, internal/lifecycle, the escalation ADR title. It is built from the FULL item ID while every display path renders through the short form, so state shows the same record at two different truncation lengths one line apart - inviting a caller to paste a long ID where a short one is expected. FIX: build the title from the short form, which is already used one line above in the same function. Titles minted before this keep the long form; nothing resolves against titles, so no migration.

TESTS: a rounds-exhausted move returns a refusal (non-zero) whose text names the refused destination and carries a callable decide object, and does NOT contain an i record line; a clean check names its counts and severities; an escalation ADR title renders at the same length as the record it names. Assert the ABSENCE of the i line explicitly - that is the defect, and a test that only checks the new text would pass with the old line still there.

VERIFY: go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l . empty. Then re-run a judge: bench -agent-prep DIR -scenario tricky, drive it, and confirm the rounds refusal is not misread. SCOPE: the two rounds returns, the check zero-findings return, the escalation title. Do NOT touch state's sections or any shape line - that is the sibling task. ROLLBACK: revert.

## T-01KYQ5047CE5MSBF7KTM3BGKVQ put the shape where the caller already is, funded by deleting output no judge read
kind: task
state: active
created: 2026-07-29
parent: P-01KYQ4YK7MEA3BP26HSQ7CWZ4R
refs: R-01KYQ4XNAFFNYSTNRKC28BR3N3
rounds: 2
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

## B-01KYQ87KTBFVVSRG337RFWCS44 rule op=edit changes a rule's text without re-stamping its anchors, leaving drift the same tool's check then refuses
kind: bug
state: draft
created: 2026-07-29
targets: internal/mcpserver, internal/drift

REPRODUCED while adding SRF-001. Sequence: rule op=add with applies (anchor stamped against the rule text), then rule op=edit changing pattern/system/response to fix the sentence. The edit succeeded silently. The very next check reported d audit SRF-001 go:mcpserver.roundsRefusal ... tightened, and the repositorys own TestCheckOnOwnRepo failed with unexpected drift audit on own repo. Re-issuing rule op=edit with the SAME applies list re-stamped the anchor and cleared it.

OBSERVED vs EXPECTED. Observed: an edit that touches only the rule SENTENCE leaves every anchor stamped against the old sentence, so the rule is immediately in an audit-class drift state (tightened blocks, per the auditGate contract) with no signal at edit time. Expected: either the edit re-stamps the anchors it already has, or it says it did not and names the follow-up. The current behavior lets a caller edit a rule into a state the same servers check refuses, and only discover it on the next check - or, as here, in CI.

WHY IT MATTERS. Editing a rules wording to fix a lint finding or an awkward sentence is a NORMAL, encouraged action - the composer even prepends The, so a first attempt often needs one. That routine action silently arms a gate. The cost is not the re-stamp itself but the discovery: the failure surfaces far from its cause, attached to a different tool.

DIRECTION, not a decision. Re-stamping automatically is the obvious fix but is not obviously right: an anchor exists so a human notices when a rule and its code drift apart, and a text edit is exactly when that judgment might be wanted. So the choice is (a) re-stamp on edit and treat the sentence as authoritative, (b) refuse the edit while anchors are stamped against the old text and say so, or (c) allow it but return a line naming the anchors now stale and the exact call that re-stamps them. (c) preserves the judgment and removes the surprise; (a) is cheapest; (b) is probably too strict for a wording fix.

VERIFY once decided: add a rule with applies, edit its text, and assert the chosen behavior - that check is clean afterward, or that the edit refused, or that the edit named the stale anchors and the re-stamp call.

## B-01KYQ939RXEZCA55ZGS46SYSES check path only labels the output; the scan is always workspace-wide
kind: bug
state: draft
created: 2026-07-29
targets: internal/mcpserver

Found by a validator while reviewing an unrelated change to checks clean-tree line.

OBSERVED. check with path api, path cli and no path at all return IDENTICAL rule and dir counts and identical finding lines. in.Path is used to LABEL output; the scan itself is spec.Load(s.ws.Dir), which always walks the whole workspace. So a caller who narrows to a directory gets the same answer as a caller who did not, with no signal that narrowing did nothing.

EXPECTED, one of two, and this needs a decision rather than a patch: either path scopes the scan (findings, coverage gaps and counts all restricted to that subtree), or the argument is removed so nobody can believe it does. A third option - keep it and say it is advisory - is the worst of the three, because the render currently juxtaposes the queried path with global counts, which reads as scoped.

MITIGATED, not fixed, in the meantime: the clean-tree line no longer prints the path beside the counts, so at least the misleading juxtaposition is gone (the counts render bare). The argument still exists and still does nothing.

WHY IT MATTERS beyond tidiness. check is the loops verification step and the thing CI gates on. A caller scoping to the directory they touched, seeing counts that look scoped, and concluding their subtree is clean, has been told something false about the rest of the workspace - or, if they read it the other way, has been given a global answer they did not ask for. Neither is a good outcome for a verification command.

VERIFY once decided: for scoping, a workspace with a finding outside the queried path reports it with no path and does NOT report it with the path; for removal, the argument is gone from the schema and the docs.

## B-01KYQA4WXEFATTX2FV30DATGDJ TestPrepIgnoresHarnessArtifacts flakes in CI: t.TempDir cleanup races git's background writes into .git/objects
kind: bug
state: draft
created: 2026-07-29
targets: internal/bench

OBSERVED in CI run 30468777911, on a branch whose diff does not touch this test or anything it exercises:

--- FAIL: TestPrepIgnoresHarnessArtifacts (1.68s)
    testing.go:1369: TempDir RemoveAll cleanup: unlinkat /tmp/TestPrepIgnoresHarnessArtifacts.../001/.git/objects: directory not empty

The assertion did not fail - the TEST BODY passed and the failure came from t.TempDirs deferred RemoveAll. The fixture git-inits and commits inside the temp dir; git leaves background work (gc, pack) that keeps writing into .git/objects after the command returns, so cleanup races it. Passes locally, including under the exact CI invocation (go test -coverprofile), which is characteristic: the race needs a slower or more contended filesystem to lose.

WHY IT MATTERS more than an ordinary flake. This test is inside make cover, which is a REQUIRED CI gate. A flake there blocks a merge that has nothing to do with it, and the failure text points at testing.go rather than at anything a reader would connect to git - the first response is to look for a real regression in the diff, which is exactly the wasted work a gate should not manufacture. It cost that here.

DIRECTION. The durable fix is to stop git from leaving background work in a throwaway fixture: git init with gc disabled (gc.auto=0), and/or commit with the maintenance/auto-gc paths off, so nothing is still writing when the test returns. A t.Cleanup that waits or retries the removal treats the symptom and still leaves a race. Whichever is chosen, apply it to every bench fixture that git-inits, not just this test - the others differ only in timing.

VERIFY: the fixture creates no background git process (assert gc.auto is 0 in the created repo), and the test survives a loop under -count=20 on a loaded machine.

## B-01KYQG88GZEM2ARX29J4ADQCX5 wall-clock assertions in the required CI gate flake on a loaded runner
kind: bug
state: done
created: 2026-07-29
targets: internal/store, internal/bench

SECOND instance of the same class in one session, so it is filed as a class rather than as another one-off.

OBSERVED. TestSQLiteStoreBatchedPutsAreFast failed in CI run on spectackle/B-01KYPC11VKF0Q-close after 19.44s, on a branch whose diff does not touch internal/store. It passes locally and passed on a rerun of the same commit. The name says what it asserts: a wall-clock upper bound. A GitHub runner is a shared, contended machine, so the bound holds or not depending on neighbors, not on the code. The sibling instance is B-01KYQA4WXEFAT, a fixture whose t.TempDir cleanup races gits background writes - different mechanism, same consequence.

WHY IT MATTERS. Both live inside make test / make cover, which are REQUIRED gates. A flake there blocks a merge that has nothing to do with it, and the first response is always to look for a real regression in the diff - this session spent two separate investigations doing exactly that, one per instance. A gate that fails for reasons unrelated to the change teaches its operator to retry rather than to read, which is precisely the habit that lets a real failure through.

DIRECTION. Wall-clock is the wrong instrument for what these tests want. A batched-put test is really asserting that the batch path does ONE transaction rather than N, which is observable directly - count the transactions, or compare the batched path against a deliberately unbatched one and assert a RATIO, which is stable under contention because both sides slow down together. If a wall-clock bound is genuinely wanted, it belongs in a benchmark that is recorded and compared over time (the bench record type exists for exactly this), not in a boolean gate.

SURVEY FIRST, then fix: grep the suite for other time.Since / Duration comparisons in assertions. Fixing one and leaving three is the same outcome as fixing none, because the gate is only as reliable as its flakiest member.

VERIFY: the chosen assertion survives a loop under -count=20 with the machine deliberately loaded, and no test in the required gate asserts an absolute wall-clock bound.

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

## B-01KYR01E2VFEF8KT5GV91VAWSP find with an empty q answers ok no matches while the workspace has rules — a silent lie, worse than a refusal
kind: bug
state: active
created: 2026-07-29
targets: internal/mcpserver

Found by an independent judge driving the tricky scenario from tool output alone, and called the WORST issue of that run.

OBSERVED. find {q: \"\", scope: \"rule\"} returns exactly ok no matches, on a workspace where state had just reported rules total=8 dirs=5. An ok plus no matches is indistinguishable from this workspace has no rules. The judge recovered only because it remembered the state count and happened to guess the search term api; its own conclusion was that a user who trusted this would conclude there was nothing to model their new rule on.

WHY IT IS THE WORST CLASS. It is not a refusal and not an error - it is a successful call returning a factually false answer, with no hint that the empty q was the problem. A refusal would have taught the caller what to supply. This is the same shape as the accepted-but-did-nothing dead ends judges rank above ordinary refusals in severity, and it sits on find, the tool the loop opens with (find scope=rejection|history is the documented learn-before-planning step).

RELATED, same judge, same root: there is NO discoverable way to enumerate rules. Every find mode requires a substring guess, and scope=rule looks like it should enumerate but does not. The judge only located the existing api rules because the goal text named the directory.

DIRECTION, and these are alternatives not a list: (a) refuse an empty q, naming what to pass - the minimum, and it at least stops the lie; (b) treat an empty q as list-all within the scope, which also closes the enumeration gap in the same change and is what a reader plainly expects from scope=rule; (c) keep requiring q but document a wildcard in the shape line. (b) is the strongest: it makes the obvious reading correct instead of teaching a workaround. Whichever is chosen, the invariant is that no query which was never executed may answer ok.

VERIFY: on a workspace with rules, find with an empty q must not answer ok no matches - it either refuses with what to pass, or returns the rules; and a scope with genuinely zero records still answers no matches truthfully.

## B-01KYR02HQ3F8KAW6JR3VSY4XVR move hides its destination enum while draft and rule inline theirs, and the EARS pattern letters ship with no legend
kind: bug
state: done
created: 2026-07-29
rounds: 1
targets: internal/spec, internal/workspace

ROOT CAUSE of a whole sessions worth of duplicate intent lines, found after the per-write guard was already fixed and duplicates kept appearing.

.spectackle/.gitattributes declares merge=union for journal.ndjson and bench.ndjson - correct, they are append-only logs where a union is exactly right and a duplicate line is faithful. spec.md and work.md have NO strategy, so they take gits default three-way merge.

That is fine for spec.mds rule sections, which are genuinely edited and need a real merge. It is wrong for its ## intent section, which is append-only in practice: every archive appends one line and nothing ever edits an existing one. Two branches that each append a DIFFERENT line produce an interleave or a conflict; two branches that each append the SAME line at a different position produce TWO COPIES, because git sees two independent insertions at two locations and keeps both. Reproduced here: lines 261 and 264 of the root spec.md are the same record with two other records between them.

WHY THE PER-WRITE FIX CANNOT COVER THIS. B-01KYQJDJJVFC2 made AppendIntent idempotent by scanning the section before writing. That guard is per-WRITE and inside one working tree; it cannot see the other branchs write, and by merge time both writes have already happened. So the guard is necessary and not sufficient, and every worktree-based archive - the documented primary workflow - can reintroduce a duplicate that no single write path is in a position to prevent.

DIRECTION, and the choice is a real one:
(a) A custom merge driver for spec.md that unions and dedupes the intent section while merging the rest normally. Correct but adds a driver every clone must configure, and an unconfigured clone silently falls back to the broken default - which is the worst property a records format can have.
(b) Move intent lines OUT of spec.md into their own append-only file with merge=union, deduped on READ. Then the format matches the access pattern (append-only, one statement per record), the merge strategy is declarative and needs no local configuration, and duplicates become harmless rather than merely prevented. Costs a format change and a migration for existing bundles.
(c) Keep the file and dedupe on read wherever the intent section is consumed, accepting duplicates on disk. Cheapest, but it leaves a permanent human-readable artifact - the one thing intent lines exist to be - visibly wrong in every diff.

(b) is the shape the evidence points at: the journal already proves the pattern works, since merge=union plus faithful-duplicate semantics has caused no trouble at all this session while spec.md caused several rounds of it.

VERIFY once decided: two worktrees each archive a different record, merge both to main, and assert one line per record and no conflict; then two worktrees each archive the SAME record and assert one line survives.

## B-01KYPC11VKF0QBF0HCPY3QCRJE Goal and Rules are parse-only: three gate paths read a field no tool can set
kind: bug
state: done
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

## B-01KYRJ3WSCE148S8S2GRWXFDJ4 the journal-compaction advisory suppresses check ok summary and prints twice, turning the required CI gate red
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/validate.go, .github/workflows/ci.yml

OBSERVED. Once the journal passes the compaction threshold, check stops emitting its summary line entirely and emits only advisories, with the journal advisory DUPLICATED:

  c . journal 504 events since last compact
  c . journal 504 events since last compact
  (no ok line, exit 0)

EXPECTED: the ok check 0 findings (E=0 W=0) - N rules N dirs summary, with advisories in addition to it, not instead of it.

WHY IT MATTERS. The CI self-hosting gate asserts the exact shape len(lines)==1 and lines[0].startswith(ok check ) and 0 findings (E=0 W=0) in lines[0]. Three lines and no ok line fails that test, so the required gate goes red on a workspace whose code and spec are both clean. The trigger is elapsed journal events, not a code change, so it fires on whatever branch happens to cross the threshold - and it blocks the archive edge, which waits on CI. It did exactly that here: the rule op=add that recorded RECMERGE-002 pushed the count past the threshold and the next archive attempt could not have merged.

ISOLATED. Not a regression from the current branch: the pre-fix baseline binary, built -buildvcs=false from the parent commit, produces the same output on the same workspace (plus a staleness hint of its own). compact {apply:true} clears it and check immediately returns the single clean ok line - 99 rules 24 dirs - so the summary is being displaced by the advisory, not lost to a scan failure.

TWO DEFECTS, likely one cause. (1) The advisory path returns early or overwrites the summary instead of appending to it. (2) The same journal advisory is emitted twice for the same dir - a duplicate the caller pays for on every check while the condition is live.

FIX DIRECTION. Advisories and the summary are different record types (c versus ok) and must compose: emit every c record AND the ok line. Then decide what the gate asserts - either it accepts advisory lines alongside the ok summary, or check gains a mode that suppresses advisories for gate use. The gate asserting a single line is what turned a soft nudge into a hard failure, so whichever is chosen, the CI expression and the render have to be changed together. Dedupe the advisory per dir.

VERIFY. A test that seeds a journal past the threshold and asserts check output contains BOTH the c advisory and the ok summary, exactly one advisory per dir, plus a test that runs the CI gate expression against that output. The current suite passes with the ok line absent, which is why this reached a red gate.

## B-01KYRN4VBEEXQ8ZVMCR1WCTPTX a heading-shaped body line forges a phantom record and steals the host body, and dir accepts a newline
kind: bug
state: draft
created: 2026-07-30
targets: internal/item/item.go, internal/mcpserver/tools.go

Two findings from independent verification of B-01KYN3E973F20, both confirmed pre-existing on the pre-fix binary and both the same class as that bug: a value that the writer emits and the parser then reads as STRUCTURE.

O1, heading injection through body. draft {body: "## T-9999 phantom\nkind: bug\nstate: archived"} exits 0 and is accepted. On reload the injected line matches reItemHeading, so the parser starts a NEW item block there: a phantom record appears with the injected kind and state, and the host record loses its entire body to the phantom. This is the same outcome the targets fix in B-01KYN3E973F20 closed, reached through a field that is free-form by design. That is why it is separate rather than an oversight in that fix - the header guard has no business refusing body prose, so the answer has to be different: escape or indent a body line that matches reItemHeading on write, or refuse a body that would parse as a new record. A phantom record in a terminal state is worse than a mangled body, because state is what the state machine trusts.

O2, newline in dir. draft {dir: "a\nb"} exits 0 and creates a directory literally named with an embedded newline, and the resulting state record line is split in two - so the dense line grammar itself is broken for that record, which is what every caller parses. dir is not stored in the machine header, which is why the header guard does not see it.

WHY BOTH MATTER. The record grammar is line-oriented end to end. Guarding only the machine header covers where the reported corruption happened, not every place a caller-supplied newline becomes a line break in output an agent parses.

FIX DIRECTION. Treat the line grammar as the invariant rather than the header: refuse or escape a newline in any caller-supplied value that reaches a rendered record line (dir is the clear case), and handle the body separately since it legitimately holds prose - most likely by refusing a body line that matches reItemHeading, which is a narrow and explainable rule.

VERIFY. A test asserting a heading-shaped body line does not produce a second item on reload and does not empty the host body, and a test asserting dir with a newline is refused. Both must exit non-zero per SRF-001.

## B-01KYS6ZKRQEHWAFHN0MD67NQY3 the parent archive child fold is a second ungated archive path, and a compensated archive keeps two of the three effects it says it refused
kind: bug
state: draft
created: 2026-07-30
targets: internal/lifecycle/lifecycle.go, internal/mcpserver/tools.go, internal/spec/author.go

Two adversarially verified findings that share one cause: the archive effects are not transactional and the gates sit on only one of the two paths that run them.

FINDING 1, HIGH, gate bypass in two calls. Every archive gate lives in the mcpserver move handler and keys on the item NAMED in the call. lifecycle.archive() then folds every done child away with a journal EvArchive plus item.Remove, with no gate at all. So parenting a record to any item and moving THAT item to done archives the child through the parent, skipping three gates: the research-consumption gate that is documented as hard regardless of feedback config, the feedback.validate=require verdict gate, and the child own open-children gate - the fold archives a child whose direct archive the server would refuse. Two calls, no refusal, exit 0.

FIX DIRECTION: the gates must move to where the effect happens rather than to where the call names an item, or the fold must run each child through the same gate set and refuse the parent transition if any child fails. The second is likely correct - a parent cannot legitimately archive a child that is not itself archivable - and it makes the refusal name the child.

FINDING 2, HIGH, compensation is partial while claiming to be whole. When the archive edge strands, tools.go compensates the item archived back to done and refuses with archive refused whole. Two effects the same call already committed are NOT undone. First, done children folded away by lifecycle.archive() stay archived and become unreachable - move on them returns unknown item - so a REFUSED transition permanently destroys sibling records. Second, spec.AppendIntent line is not rolled back, and because AppendIntent dedupes by record ID, the successful RETRY note is silently discarded and the living spec permanently records the FAILED attempt note as the item outcome. This is the untriaged suspicion already noted in work.md, now reproduced, and the intent-note freeze is the part nobody had suspected.

The phrase refused whole is therefore untruthful in the SRF-001 sense: it names an outcome the code did not deliver. Either the effects become transactional - stage the fold and the intent append, commit them only when the edge succeeds - or the refusal must enumerate what it could not undo. Given the retry after a stranded closure is documented as normal operator behavior, the first is the real fix; the dedupe-by-ID behavior means the note damage is silent and permanent, which is the worst combination.

RELATED: move to=rejected has no open-children gate at all - lifecycle.Move guards openChildren only for archived - so rejecting a parent orphans live children whose parent then resolves to neither work.md nor a tombstone, and Draft refuses to add any new child under it. Two boundaries both remove a record from work.md and only one checks children.

VERIFY. A parent with a done child that would fail its own archive gate: archiving the parent must refuse and name the child. A stranded archive: assert the folded children are still live, that a retry appends the retry note rather than keeping the failed one, and that the refusal enumerates anything it could not undo. Rejecting a parent with live children must be gated the same way archiving is.

## B-01KYS7111XFHZVZ4CRKYQ3KR7R decide op=answer accepts any string on a rounds-escalation ADR, burns the decision and strands the item in blocked forever
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/decide.go, internal/lifecycle/lifecycle.go

HIGH, adversarially verified, and it makes the one documented exit from blocked unreachable.

lifecycle.Escalate writes the escalation ADR body with choose=rescope-pipe-reject-pipe-override-once. decideOptions third parser is a regex looking for outcome= and therefore never matches that body. decideOptions returns nil, and decide.go no-match branch treats the ADR as free text and accepts ANY choose value. resolveDecision then marks the ADR done with status accepted, and because the choice is not one of the three recognized outcomes it takes the else branch, clearing the ADR from the blocked item needs WITHOUT calling ResolveBlocked. The call exits 0 with a success-shaped ok ADR followed by the junk value.

The item is now permanently blocked: every move from blocked refuses by design since only ResolveBlocked can move an item out, re-answering the ADR refuses because it is already decided, and the needs link that would have driven resolution has been cleared. There is no recovery through any public tool. A single typo in a choose value - the exact case HINT-001 exists to make cheap - is unrecoverable, and it reports success while doing it.

THREE DEFECTS IN ONE PATH. First the option parser does not recognize the body the escalation writer produces, which is the same writer-reader disagreement class as the header round-trip and the truncation marker: two halves of one program describing the same artifact differently. Second the free-text fallback is applied to a record that DOES have an enumeration, so validation is skipped exactly where it matters. Third the else branch clears needs on a path that did not resolve anything, which is backwards - and the inverse is also true: the resolving paths keep the spent ADR in needs while the non-resolving path clears it, so the bookkeeping is inverted on both sides.

FIX DIRECTION. The escalation body and the option parser must share one composer, so a change to either cannot silently desynchronize them - the same fix shape RECMERGE-002 and the marker pin used. Then an unrecognized choose on an ADR that HAS options must be an ARG E refusal that teaches the three values per HINT-001, not a free-text acceptance. Then needs must be cleared only by a path that actually resolved the block. Finally decide the recovery story for records already stranded this way: a repair op, or a documented manual path, because the fix alone does not free them.

RELATED, MEDIUM, journal truthfulness: lifecycle.Move appends the EvMove done-to-active event BEFORE evaluating the rounds budget, so when the budget is exhausted the move is refused and the item stays on done, yet the journal permanently asserts a done-to-active transition that the tool rejected. internal/replay handles EvEscalate explicitly so final-state-wins recovers, but any reader of find scope=history or the raw journal sees a transition that did not occur. Append after the decision, not before it.

VERIFY. Answering an escalation ADR with a value outside the three must refuse, exit non-zero, teach the enumeration, and leave both the ADR and the item untouched. Answering with each valid value must reach the state that value promises. A test must assert the escalation body and the option parser agree, by construction rather than by two literals. The rounds-exhausted path must journal no move event when the move was refused.

## B-01KYS711ZFFG0SJ7HY7DSANGFN the largest per-session token cost is the tools/list schema surface at 19.9KB, and nothing measures it on either the cost or the benefit side
kind: bug
state: draft
created: 2026-07-30
targets: internal/bench, internal/mcpserver/tools.go

HIGH, and it reframes every byte-economy conclusion this repository has drawn.

MEASURED. The bench manifest line presents 4299B as THE once-per-session cost. A real MCP connect handshake writes 26084B, of which tools/list is 19908B - 76 percent - and the manifest only 4561B, 17 percent. Nothing in internal/bench reads tool descriptions or schemas, and the judge harness deliberately hands the agent tool NAMES ONLY. So the roughly 20KB tool-description surface is invisible on the cost side AND on the benefit side simultaneously: the benchmark cannot see what it costs, and the judge never reads it so cannot show what it buys. Every conclusion drawn so far concerns 3039B of call results plus 4299B of manifest, while 19908B of schema sits outside the frame - 6.5 times the entire scripted total.

WHY THIS MATTERS MORE THAN ANY SINGLE TRIM. The standing objective is the best valid and complete result per token. A metric that omits 76 percent of session cost cannot rank surface changes correctly, and BENCH-001 - revert a surface change whose judged metric did not improve - is only as trustworthy as the metric. Two of the fattest metered lines are landed judge FIXES: the 246B VALIDATE W advisory and the 58B gloss on the ROUNDS refusal both exist because judges misread shorter versions. Trimming them measures as a win and would be a regression, which is BENCH-001 inverted - the benchmark sanctioning a regression rather than a bogus improvement. The probe measured both to show the trap and explicitly did not advocate either.

WHERE THE METERED BYTES ARE, for whoever works this next. Five steps carry 50.2 percent of 3039B: state/final 380, move/T1-archived 321, grill/T1 286, find/rejections 277, escalate/T2 262. By class: item i records 1095B or 36.0 percent, journal j and sw 448B, refusals 396B, ok summaries 390B, the single VALIDATE W advisory 246B, git g records 175B, section headers 79B, next hints 79B. The largest single LINE is that one advisory; the largest CLASS is 19 i records re-emitting kind, scope and title on every transition of the same item.

MEASURED CANDIDATES, none yet applied. Manifest paragraph 1 spends about 635B restating what tool descriptions already carry in the same handshake; a rewrite keeping the loop order, the file-layout sentence, the record alphabet and the cur rule measures manifest 4299B to 3713B with per-call total unchanged. Normalizing journal j, swarm sw and several refusal lines to the existing short ID prefix instead of the full 28-character ULID measures -52B at valid=true. state #version plus section headers are 86B of every state call and both trims measure clean, but both are pinned by SPX-MCP-005 at E severity, so they are byte opportunities that cannot be taken as text edits.

FIRST TASK IS INSTRUMENTATION, NOT TRIMMING. Teach the bench to meter the real handshake - tools/list plus manifest - so the denominator matches what a session actually pays, and give the judge harness the real tool descriptions so their guidance value becomes measurable at all. Only then is a schema trim rankable. Until then any tool-description edit is unmeasured by construction, and BENCH-001 cannot adjudicate it.

ALSO: HINT-001 is only half-satisfied. move, rule and knowledge teach the enumeration on the refusal that rejects a wrong value; draft kind and find scope name the bad value and stop, so a wrong guess costs a blind retry - the correction-round cost the objective subordinates per-call bytes to.

## B-01KYSB0BAAEB2BYNX4YYRQZEE4 the short-prefix collision probability in ids is wrong by 16x, and bench renders a fixed prefix instead of the adaptive one so a same-millisecond pair flakes the suite
kind: bug
state: draft
created: 2026-07-30
targets: internal/ids/ids.go, internal/bench/bench.go

Both found by the independent verifier of B-01KYS6Y5NKF42, measured rather than reasoned, and both pre-existing rather than caused by that change - but the first became load-bearing when attribution started depending on the short prefix.

FINDING 1, the documented collision probability is off by 16 times. internal/ids states the 13-character short prefix leaves a 2 to the minus 15 collision chance, about 1 in 32768. Measured over 2 million same-millisecond pairs: 1 in 2070. The analysis error is identifiable: a 13-character Crockford base32 prefix pins 63 bits, but 4 of them are the FIXED UUIDv7 version nibble, so only 11 random bits are actually pinned, not 15. Every place that reasons about short-prefix safety inherits the wrong number.

WHY IT MATTERS NOW. B-01KYS6Y5NKF42 changed the validation attribution grep from the full record ID to the short prefix, which is the only way it matches what the code-commit writers produce. That makes prefix collisions a silent cross-attribution vector: two records whose short forms collide each inherit the other commits in their attributed diff. The residual is now written down in validate.go with the corrected number, and the failure direction is conservative - a larger diff means more staleness, more risk trips, more findings - with one permissive edge where a colliding sibling touching a declared target can mask an untouched finding. But the constant that everyone reasons from should be right, and the doc comment should carry the version-nibble explanation so the mistake is not repeated.

FIX: correct the number and the derivation in the doc comment. Then decide, with the corrected figure in hand, whether 13 characters is still the right adaptive floor - 1 in 2070 for same-millisecond mints is a different design conversation than 1 in 32768, especially in a swarm that mints in parallel.

FINDING 2, a live intermittent suite failure. TestBenchCmpDeltasAndUnitMismatch fails intermittently with ids: prefix matches 2 records, naming two IDs that differ only past the 13th character. Cause: internal/bench renders shortDisplayID, a FIXED 13-character prefix, where it should render the adaptive ShortenRecordID that widens the prefix until it is unambiguous in the current workspace. The adaptive shortener exists precisely for this. Observed 1 failure then 8 of 8 passes in isolation and a clean second full run, so it is a genuine flake keyed on two bench records minted in the same millisecond.

FIX: use the adaptive shortener in the bench render paths. VERIFY: a test that mints two records in the same millisecond and asserts every rendered ID resolves to exactly one record.

METHOD NOTE worth keeping: this flake was invisible to the harness used all through this session, because piping go test into a filter and echoing a marker afterwards discards the exit code - the filter still prints FAIL lines, so a failure is visible if it occurs, but the run is never actually asserted to have passed. Assert the exit code.
