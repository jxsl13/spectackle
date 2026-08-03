---
schema: v1
---

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

## B-01KYSB0BAAEB2BYNX4YYRQZEE4 the short-prefix collision probability in ids is wrong by 16x, and bench renders a fixed prefix instead of the adaptive one so a same-millisecond pair flakes the suite
kind: bug
state: draft
created: 2026-07-30
targets: internal/ids/ids.go, internal/bench/bench.go

Both found by independent verification of B-01KYS6Y5NKF42, measured rather than reasoned, and both pre-existing rather than caused by that change - but the first became load-bearing when attribution started depending on the short prefix.

FINDING 1, the documented collision probability is wrong by 16 times. internal/ids MinRecordPrefixLen doc comment claims the 13-character short prefix leaves a 2 to the minus 15 collision chance, and ids_test.go TestPrefixPinsFivePMinusTwoTimestampBits mirrors the claim. The true figure is 2 to the minus 11.

EXACT DERIVATION, independently reproduced twice. A 13-character Crockford base32 prefix pins 5 times 13 minus 2 equals 63 bits, per the formula ids.go itself uses - two slack bits in the first character. Against MintRecordIDAt byte layout: bytes 0 through 5 are the 48-bit timestamp; byte 6 high nibble is the UUIDv7 version, hard-fixed to 0111, and it falls INSIDE the 63-bit window; byte 6 low nibble is 4 random bits; byte 7 contributes 8 random bits of which only the first 7 fall inside the cutoff, since 63 equals 48 plus 8 plus 7. The variant bits in byte 8 start at bit 64 and are outside the window entirely. Random bits inside the prefix: 4 plus 7 equals 11, not 15.

MEASURED, by instrumenting MintRecordIDAt directly: 2 million same-millisecond mints produced exactly 2048 distinct 13-character prefixes, which is 2 to the 11; 5 million independent same-millisecond pairs collided at 1 in 2048.3. So the exact constant is 1 in 2048, and the earlier 1 in 2070 figure was a measurement approximation about 1 percent off.

WHY IT MATTERS NOW. B-01KYS6Y5NKF42 changed the validation attribution grep from the full record ID to the short prefix - the only form that matches what the code-commit writers produce. Prefix collisions are therefore a silent cross-attribution vector: two records whose short forms collide each inherit the other commits in their attributed diff. That residual is now stated in validate.go itemDiff. The failure direction is conservative - a larger attributed diff means more staleness, more risk trips, more findings - with one permissive edge confirmed in code: validateComputed skips the untouched finding for a target whenever the attributed file set already contains that path, so a colliding sibling touching a declared target masks it.

FIX: correct the constant and carry the derivation in the doc comment, including the version-nibble point, so the error is not re-derived. Correct ids_test.go, which currently pins the wrong claim. Then decide, with the right number in hand, whether 13 characters is still the correct adaptive floor - 1 in 2048 for same-millisecond mints is a different design conversation than 1 in 32768, particularly for a swarm that mints in parallel by design.

FINDING 2, a live intermittent suite failure. TestBenchCmpDeltasAndUnitMismatch fails intermittently with ids: prefix matches 2 records, naming two IDs that differ only past the 13th character. Cause: internal/bench renders shortDisplayID, a FIXED 13-character prefix, where it should render the adaptive ShortenRecordID that widens until unambiguous in the current workspace. The adaptive shortener exists for exactly this. Observed once, then 8 of 8 passes in isolation and clean full runs after, so it is a genuine flake keyed on two bench records minted in the same millisecond - and by the corrected figure that is a 1 in 2048 event per pair, not 1 in 32768.

FIX: use the adaptive shortener in the bench render paths. VERIFY: a test that mints two records in the same millisecond and asserts every rendered ID resolves to exactly one record.

METHOD NOTE worth keeping: this flake was invisible to the harness used through the session that found it, because piping go test into a filter and echoing a marker afterwards discards the exit code. The filter still prints FAIL lines so a failure is visible when it happens, but the run is never actually asserted to have passed. Capture the output and read the exit code directly.

## B-01KZ10JP6BFV9B5EGWVVP6R8H3 two merged records carry validation verdicts from orchestrator-spawned validators, which the tombstones do not disclose
kind: bug
state: draft
created: 2026-08-02
refs: B-01KYRN44EVEK2B0Q772MFEPZWK, B-01KYZB4QA9FF4TCA3AQGWT7E5D
targets: .

The journal for two merged records implies an independent validation verdict that was not independent in the sense the gate means. Filed so later sessions do not read more assurance into those tombstones than the evidence supports. User-authorized on 2026-08-01 to leave both merged and record this note; the standing policy is now VALIDATOR-PROVENANCE-001.

AFFECTED. B-01KYRN44EVEK2 (pr 252) and B-01KYZB4QA9FF4 (pr 253). Both archived on verdicts recorded by validator identities that an orchestrator spawned and named (verifier-w1, verifier-await-2). feedback.validate=require exists to demand a second PARTY before archive; it was handed a second PROCESS. A safety classifier caught this on the third record and refused to record that verdict, which is how it surfaced.

WHAT THE EVIDENCE ACTUALLY IS, so the correction is not overstated in the other direction. The verification was technically real and adversarial, not a rubber stamp. On B-01KYZB4QA9FF4 the panel returned 0/3 and REFUTED the implementation, reproducing against the real forge.GitHub over httptest a regression that would have made the archive gate refuse any build whose CI had not yet been dispatched - B-01KYQJDJJVFC2 rebuilt inside its own fix, with a test that pinned the wrong behavior. That defect was found and corrected BEFORE merge because of this process. On B-01KYRN44EVEK2 two lenses independently flagged an overbroad comment. So the finding is narrow: the verdicts are not worthless, they are misattributed.

NOT A CODE DEFECT. Nothing in the server behaved incorrectly - the gate refused an anonymous validator exactly as designed and had no way to distinguish a spawned agent from any other second identity, which is the open question rather than a bug. Whether an identity check SHOULD be able to tell them apart is a separate design question and is not proposed here.

VERIFY: this record is discoverable from a search for validation provenance, and names both affected record IDs and both PR numbers, so a session auditing either tombstone finds it.

## B-01KZ13VMF0EXP9691Y21868ZQS TestConcurrentApplyMintsOneDecision flakes in CI: the racing applies resolve correctly but the surviving decision is invisible to the querying session
kind: bug
state: draft
created: 2026-08-02
refs: B-01KYQG88GZEM2ARX29J4ADQCX5, B-01KYQA4WXEFATTX2FV30DATGDJ, B-01KYSX35RKFYBRX6YAB9E9DHBW
targets: internal/mcpserver, internal/knowledge

THIRD member of the CI-flake class already filed twice (B-01KYQG88GZEM2 wall-clock assertions, B-01KYQA4WXEFAT TempDir/git race), and the first whose mechanism is cross-session record visibility rather than timing of the assertion itself.

OBSERVED, GitHub Actions run 30745293519 on branch spectackle/B-01KYSX35RKFYB, in the make cover step:
  --- FAIL: TestConcurrentApplyMintsOneDecision (0.05s)
      knowledge_test.go:874: two racing applies must leave exactly one decision, got []
The captured tool output in the same failure shows the concurrency resolution WORKED. Racer B minted one ADR and reported added=1 conflicts=1 with need decision ADR-01KZ132ZN5F8R. Racer A saw it, correctly yielded, and reported added=0 settled=1. Exactly one decision existed. The assertion still failed because decisionIDs, queried through A's session, returned an EMPTY list - so the ADR that demonstrably existed was not visible to the querying session at that moment.

So the defect is NOT double-minting, which is what the test was written to catch and what its name says. It is that a record minted through one session is not reliably visible to a query on another session immediately afterward. The test name and its failure message both point at the wrong mechanism, which will mislead the next person who sees it red.

NOT CAUSED BY THE CHANGE IT BLOCKED. It surfaced while archiving B-01KYSX35RKFYB, whose only edit is Server.resolveDecision. knowledge apply never calls resolveDecision. Measured: the identical head was re-run and PASSED (same run id, same SHA, rerun --failed), which is the definition of intermittent.

NOT REPRODUCIBLE LOCALLY on an M-series laptop: 15 runs plain, 10 runs with -race, and 15 runs of the same test against origin/main without the change, all green. A shared, contended runner is the only environment that has produced it, exactly like the two prior members of this class.

RELATED, and probably the same root: the implementer of B-01KYSX35RKFYB had to nil Server.scCache in test helpers because that field memoizes the known-ID set for one tool call, and a cached scope can predate an ADR minted between calls. That is the same staleness in a form the tests could see and work around.

DIRECTION, not decided here. Either the query path must be made to observe a just-committed record deterministically (find the memo or index refresh that a second session misses, and make the mint publish through it), or - if cross-session immediacy is genuinely not promised - the test must stop asserting it and the promise must be written down. What must NOT happen is a retry loop in the test: that converts a real visibility question into a hidden sleep, and this class already has two members that were only found because they failed loudly.

VERIFY: the mechanism is named correctly (visibility, not double-mint) in whatever fix lands; the test either observes a deterministic publish point or is rewritten to assert only what the system promises; and 200 consecutive runs under artificial CPU contention stay green.

## B-01KZ16J3PKERRV6E7BB8F3BZJK a single-segment directory target such as docs is honored by the done gate and silently dropped by validate, so every documentation change manufactures an offscope finding
kind: bug
state: draft
created: 2026-08-02
refs: B-01KYRVXQ02FDH9YBAFG64SH13N
targets: internal/mcpserver

Two gates compute declared-target scope differently, and the disagreement is total for any SINGLE-SEGMENT directory target: docs, cmd, poc, bin, examples. The done edge honors it; validate silently drops it and reports every file under it as offscope.

ISOLATED CAUSE, one expression. internal/mcpserver/tools.go:2852, targetPath ends with
    return t, strings.ContainsAny(t, "./")
The heuristic is "a target is a path when it contains a dot or a slash". internal/mcpserver contains a slash and passes; docs contains neither and returns ok=false. validate.go:344-352 iterates targets and `continue`s on !ok, so the target is not merely unmatched - it is removed from consideration, and the file then falls through to `if !in && len(it.Targets) > 0` and is emitted as `v offscope`.

The other gate never consults targetPath. inTargetScope (internal/mcpserver/gitflow.go:1193-1204) does a plain prefix match: f == t || strings.HasPrefix(f, t+"/"), plus a root-dir escape. docs/tools.md against target docs matches there.

OBSERVED end to end on B-01KYRVXQ02FDH, whose declared targets were internal/mcpserver, internal/knowledge, docs. `move to=done` ACCEPTED the changed docs/tools.md - no refusal, the transition completed. The very next `validate` render on the same item and the same diff emitted `! VALIDATE E B-01KYRVXQ02FDH unaddressed findings: offscope:docs/tools.md`. Same item, same targets, same file, opposite verdicts.

CONSEQUENCE, and why this is not cosmetic. The offscope finding must be addressed or waived before archive, so every record that legitimately edits documentation manufactures a finding whose only correct disposition is a waiver. That trains the waiver reflex on a false positive, and this workspace's own health line already warns waiver-rate 55% over the last 20 verdicts. A gate whose noise must be routinely waived stops being read.

Second-order: because targetPath is also what normalizeTargets uses (tools.go:2855-2865), a single-segment target survives normalization unchanged (the else branch keeps t), so the value looks correct everywhere it is DISPLAYED. The failure is only in the consumer that filters on ok.

NOT THE FIX, stated so it is not re-attempted: widening the heuristic to "anything without a colon is a path" collides with the node-ID form targetPath exists to split (go:pkg.Fn). The two consumers wanting different things is the actual design question - the done gate wants "is this file inside a declared area", validate wants the same, and only one of them is asking targetPath. The narrow repair is to make validate use inTargetScope, which already encodes the intended semantics including the root-dir case; the broader question is why two implementations exist at all.

VERIFY: an item declaring a single-segment directory target and changing a file under it passes the done edge AND renders no offscope finding; a genuinely undeclared file still produces one; and the root-dir target keeps allowing everything at both gates.

## T-01KZ1WJ2XGEFKB9DMGQWW62AWB characterize graph.ValidNodeID directly and pin both anchor classes in one check run
kind: task
state: draft
created: 2026-08-02
refs: B-01KYN5ZYM1FY2TBZHXC43V68TE, B-01KZ16J3PKERRV6E7BB8F3BZJK
targets: internal/graph, internal/mcpserver

Two coverage gaps left by B-01KYN5ZYM1FY2, plus the reason they are worth one record rather than a waiver.

GAP 1. graph.ValidNodeID has NO direct test. Confirmed: grep over every _test.go in the tree returns nothing. It is exercised only through drift.Classify and the two mcpserver surfaces, so its own boundary behavior is uncharacterized - missing colon, empty name after the colon, empty string, unknown lang, a lang that is a prefix of a known one, leading or trailing whitespace, and a node ID whose lang segment contains a slash or a dot. Each of those is a distinct branch of strings.Cut plus a map lookup, and none is pinned. The consequence is not hypothetical: this function decides whether a rule anchor is reported as a permanent CI error, so a false positive reddens the gate with no reindex able to clear it.

GAP 2. No single check-level test puts BOTH an unresolvable and a genuine pending anchor in ONE check run. The separation is currently pinned by one test per class. An independent validator wrote the combined case in a throwaway copy and confirmed the property holds today (one E finding, ok 1 anchors pending with count 1 rather than 2, and the state line reading pending=1 unresolvable=1), but nothing in the tree keeps it holding. The interesting failure mode is a counter that double-counts or a tally line that sums the wrong set, which per-class tests cannot see.

WHY THIS IS RECORDED AND NOT WAIVED, which is the part worth keeping. The untested:ValidNodeID finding came from the validate pack's static check. THREE independent adversarial verifiers, running 191 tool calls including six code mutations, all returned pass - and none of them noticed. They were not careless: every one of them exercised ValidNodeID through its call sites and watched a mutation there fail a test. That is exactly what mutation testing proves, and exactly what it does not: killing a mutant at a call site shows the CALL SITE is pinned, not that the function is characterized. A newly exported predicate can be fully mutation-covered through its consumers and still have no test that states what it means on its own.

So the two techniques are not redundant and neither subsumes the other. The static untested: finding is cheap and catches the class that adversarial verification structurally misses. This workspace's health line already warns waiver-rate 55 percent over the last 20 verdicts; a finding that three verifiers could not have produced is precisely the kind that must not be reflexively waived. B-01KZ16J3PKERR covers a different half of the same waiver problem - findings that are FALSE and must be waived - and the two together are the argument for looking at what the pack says rather than at how often it is right.

VERIFY: a table-driven test over graph.ValidNodeID naming each boundary case above and asserting both directions; a check-level test placing both anchor classes in one workspace and asserting the E finding, the pending tally count and the state summary counters simultaneously; and validate on a record touching internal/graph no longer emits untested:ValidNodeID.

## T-01KZ24H1F9EYY99M87KK4HR5AX pin two properties that are correct but unpinned: the escalation ADR title length, and the move route's non-zero exit
kind: task
state: draft
created: 2026-08-02
refs: T-01KYQ503AGE6TV1NWY3EAVZSA6
targets: internal/lifecycle, internal/mcpserver

Two properties of T-01KYQ503AGE6T's earlier landings are correct in the code and pinned by nothing. Both were found by an independent validator that mutated already-merged lines rather than only the diff under review, and both are invisible to CI today.

GAP 1, the sharper one. The escalation ADR title fix (D3) is entirely unpinned. Reverting internal/lifecycle/lifecycle.go:383 from shortID(it.ID) back to the full it.ID leaves ./internal/lifecycle, ./internal/mcpserver, ./internal/item and ./internal/knowledge ALL GREEN - measured, not inferred. The whole point of that change was that an escalation ADR's title renders at the same length as the record it names, and the record's own TESTS line asks for exactly that assertion. It was never written. A regression here restores the defect silently.

GAP 2. The MOVE route's non-zero exit is unpinned, which is the ironic half. That route is the one this record calls genuinely fixed and pinned, and its guard test internal/mcpserver/knowledge_test.go:1072 goes through callText - which DISCARDS res.IsError. So the test can see the refusal text and cannot see the exit code at all. refuse() does set IsError, so the behavior is right; the assertion simply is not there. Only the three routes added later assert it, via a callRaw helper whose own comment concedes the gap.

WHY THIS PAIR IS WORTH ONE RECORD. Both are the same failure: a property was fixed, described in the record, and never converted into an assertion, so the fix survives only as long as nobody edits that line. That is distinct from a missing test for untested code - here the code is right TODAY and the tests certify a neighboring property, which reads as coverage. SRF-001's exit-code half is precisely the kind of contract that decays silently, because a wrong exit code still prints the right words.

DIRECTION. Pin the title length with a test that asserts the ADR title contains shortID(it.ID) and NOT the full ID, so both halves are stated. Convert the move-route guard to assert res.IsError through a raw CallTool, reusing the callRaw helper the newer file already provides rather than adding a second one. Neither needs a production change; if either requires one, that is itself the finding.

VERIFY: reverting lifecycle.go's shortID call fails a test; converting the move-route guard makes a mutated refuse-to-text change fail on the exit code rather than only on the prose.

## B-01KZ2ND558EAJR3NHNEE8FWBHJ item.Item.Kind is the eighth field of the restore-coercion class and is still uncoerced, so a heading-shaped kind restores to an unwritable record
kind: bug
state: draft
created: 2026-08-03
refs: B-01KYRQY892FSDSN75P9FFXFDM5
targets: internal/lifecycle, internal/item

item.Item.Kind is the eighth field of the class B-01KYRQY892FSD closed for seven, and it is still live. Found by an independent validator that probed BEYOND the record's enumeration rather than stopping at it.

MEASURED. An archive or reject event carrying K = "a\nb" restores through BOTH readers - Tombstone and lastReject - to a record item.CheckHeader refuses with "kind must be a single line". That is the same terminal shape the sibling fields had: a rejected record that cannot be written and therefore cannot be revoked.

WHY IT SURVIVED THE SIBLING FIX. restoreRecord now coerces the ten fields it assigns, but Kind is not assigned there - it comes from Event.K through the readers' own struct literals, which is exactly the shape Title had before that record pulled Ti out of the literals to create a single coercion point. The same move applies: drop K from the Tombstone and lastReject literals so Kind flows through restoreRecord and is coerced once.

NOT A HIDDEN GAP. The reflect ratchet added by B-01KYRQY892FSD lists Kind in its exempt map with the note that it is assigned from Event.K by the readers' struct literals, still uncoerced, a known residual of this class. So the ratchet is doing its job - it forced the exemption to be written down instead of silently passing. This record is the follow-up that exemption points at.

SCOPE JUDGMENT, recorded so it is not re-litigated. The sibling record's VERIFY line says "ANY header field", which reads as covering Kind, but the same sentence operationalizes as "table-drive it over all seven" and its body enumerates Ti, Par, Gr, St, Tg, Refs, Nd. The validator judged that a pass with a follow-up rather than a failure, because every operative demand was met and mutation-proven and Kind is a defect the record never identified. That judgment is endorsed here; the fix belongs in its own record rather than as unscoped extra work.

DIRECTION. Coerce Kind via item.NormalizeHeaderLine on the restore path, by the same single-coercion-point move Title got. Then REMOVE Kind from the ratchet's exempt map - the exemption list is the ratchet's own honesty, and leaving a fixed field on it would rot it. Consider also whether an unknown-kind value should be refused rather than merely flattened: unlike prose, Kind is a closed enum (item.ValidKind), so a flattened but invalid kind is still a record no tool can act on. That is a real question this record should answer rather than assume.

VERIFY: an archive and a reject event whose K carries each of the hostile values already in TestRestoreRecordAlwaysWritable's table restore to a record CheckHeader accepts, through all three readers; Kind is no longer in the ratchet's exempt map; and the ratchet still fails if Kind's coercion is removed.

## B-01KZ30YP3EF5Y81J0F8CMAK190 the apply refusal headline can read refused 1 of 0 entries, because conflict-mint failures increment a counter the entry denominator does not cover
kind: bug
state: draft
created: 2026-08-03
refs: B-01KYRN43FQFZ4RCB2F1K0QBB9R
targets: internal/mcpserver

knowledgeApply's refusal headline reads "apply refused N of M entries" where M is len(toAdd.Entries), but the refused counter is ALSO incremented by the two conflict-mint failure paths, which are not entries. An artifact with zero adoptable entries and a failed conflict mint therefore renders "apply refused 1 of 0 entries" - a count that cannot be true.

Found by an independent validator while verifying B-01KYRN43FQFZ4, and disclosed as non-blocking rather than folded in silently: the exit code and the leading refusal are both correct, only the denominator is nonsense, and nothing in that record's VERIFY line covers it.

WHY IT IS WORTH FIXING ANYWAY. The headline exists to lead with what did not happen, and a reader who sees 1 of 0 learns that the number is unreliable rather than what was refused. That is the failure mode SRF-001's leading line is meant to prevent.

DIRECTION, not decided. Either count conflict-mint failures separately from entry refusals so each denominator is honest (two counters, two clauses), or widen the denominator to entries plus attempted conflict mints and say so in the wording. The first is more truthful and slightly longer; the second is one expression. Whichever lands, the wording must not imply an entry was refused when the failure was a conflict mint - that is a different thing having gone wrong and a different remedy.

VERIFY: an artifact with zero entries whose conflict mint fails renders a headline whose denominator is reachable, and an artifact with N entries of which K are refused still reads K of N.

## B-01KZ35SVM7EFBS6DE18SQS9213 ResolveBlocked reject removes a parent without the open-children gate, so it orphans live children one door past the sibling fix
kind: bug
state: draft
created: 2026-08-03
refs: B-01KYS6ZKRQEHWAFHN0MD67NQY3
targets: internal/lifecycle

A SECOND reject boundary skips the open-children gate that B-01KYS6ZKRQEHW just added to lifecycle.Move. Found by an independent validator probing past that record's enumeration, and probe-confirmed rather than inferred: calling ResolveBlocked with outcome=reject on a parent that has an ACTIVE child returns err=nil, removes the parent, and leaves the child live with a dangling parent pointer.

WHY THE SIBLING FIX DOES NOT COVER IT. B-01KYS6ZKRQEHW widened the guard inside Move to fire for rejected as well as archived. ResolveBlocked does not route through that guard - it removes the item on its own path - so the identical orphaning survives one door over. This is the same defect shape, not a new one.

THE TRAP, and it is why this needs its own record rather than a one-line copy of the sibling gate. Escalate PARENTS its auto-minted ADR to the item it blocks. A blocked item therefore always has at least one child, the decision record that is the reason it is blocked. A naive openChildren check in ResolveBlocked would trip on that ADR every time and make reject-from-blocked permanently unreachable, converting an orphan bug into a deadlock - the same class as the pre-Move scope-gate deadlock workspace.go's predicate exists to prevent.

DIRECTION, not decided. Either exclude the item's own escalation ADR from the check (it is identifiable: it is the ADR whose id is in the item's Needs), or run the check over children that are neither the escalation ADR nor already terminal. Whichever lands must be tested against a blocked item in its ordinary shape - escalated, ADR present, no other children - to prove reject-from-blocked still works, because that is the case a careless gate breaks.

VERIFY: ResolveBlocked with outcome=reject refuses on a parent with a live non-ADR child and names it; the ordinary blocked-then-reject path with only the escalation ADR still succeeds; and the sibling gate in Move is unchanged.

## B-01KZ35V1PPEPFV3V6KKZRE9S94 the archive fold skips the audit gate, so a done child with unreviewed anchor drift is removed by folding when its own archive would refuse
kind: bug
state: draft
created: 2026-08-03
refs: B-01KYS6ZKRQEHWAFHN0MD67NQY3, B-01KYQ87KTBFVVSRG337RFWCS44
targets: internal/lifecycle, internal/mcpserver

The fold path skips the audit gate, so a done child with audit-class drift folds through a gate its own archive would refuse. A FOURTH gate, not among the three B-01KYS6ZKRQEHW enumerated and closed. Found by an independent validator reading past the record's list.

MECHANISM. auditGate runs only on the NAMED item. B-01KYS6ZKRQEHW routed fold children through archiveGateGap, which carries the research and validate gates, and added a subtree walk for the open-children and orphaned-descendant gates. The audit gate was not part of either, so a child that accumulated drift since it went done is removed by the fold without ever being asked.

WHY IT MATTERS RATHER THAN BEING TIDY. Audit drift is exactly the state a human is supposed to look at: the anchor and the code disagree. Archiving that child directly is REFUSED by design (B-01KYQ87KTBFVV's whole argument is that the tightened state must not auto-heal). Folding it through the parent removes the record and its unreviewed divergence in one step, which is the same silent-destruction shape the sibling record closed for three other gates.

DIRECTION. Fold the audit gate into archiveGateGap so all four run on children by construction, rather than adding a fourth call site that the next gate can also miss. That is the structural fix: the sibling record's pattern of enumerate-the-gates is what allowed a fourth to be forgotten, and a single chokepoint every archive path shares removes the enumeration.

VERIFY: a parent whose done child carries a tightened anchor refuses to archive and names the child; the same child archived directly still refuses with the same reason; a parent whose done child is clean still folds.

## B-01KZ35WRCKFZTTVWCMS638NTKC a refused archive leaves its spec.md intent line behind, so retrying appends a duplicate every time
kind: bug
state: draft
created: 2026-08-03
refs: B-01KYS6ZKRQEHWAFHN0MD67NQY3
targets: internal/spec

RE-DRAFTED from journal create event j:.#550, which the server's health check flagged as orphaned: created in the journal, no terminal event, missing from work.md. Re-drafting rather than leaving it dangling is the remediation the health line itself names.

THE DEFECT IT DESCRIBES IS FIXED. B-01KYS6ZKRQEHW added spec.RemoveIntent and called it from CompensateArchive, so a refused archive now removes its own intent line instead of leaving it behind. The duplicate-on-retry symptom this record reports is closed by that change, and its TestStrandedClosureRollsBackIntentLine pins exactly the reported sequence: refuse, retry with a different note, and the living spec carries the RETRY note rather than the failed attempt's.

The original record predates that fix and was never promoted, so it carries no evidence the fix does not already have. It is re-drafted here only so the orphan has a terminal event rather than dangling in the journal forever, and should be closed with that citation.
