---
schema: v1
---

## B-01KYRN43FQFZ4RCB2F1K0QBB9R knowledge apply exits 0 and prints ok applied beside a per-entry refusal
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/knowledge.go

Found by independent verification of B-01KYN3E973F20, confirmed pre-existing (identical on the pre-fix binary), so it is a separate defect rather than a regression.

OBSERVED. A knowledge apply whose entry is refused prints the refusal AND a success line, and exits 0:

  ! ARG E - apply adr <key>: <reason>
  ok applied added=0 gaps=1
  (exit 0)

EXPECTED per SRF-001: a refused operation exits non-zero and leads with what did NOT happen, never rendering a success-shaped line for a state the caller did not request. draft, decide and move all get this right - they exit 1 with no record line. knowledge apply is the outlier.

WHY IT MATTERS. An agent that checks the exit code sees success; an agent that reads the last line sees ok applied. Either way the refusal is invisible, so an import silently drops entries and the caller believes the artifact landed. added=0 is the only signal, and it is easy to read as nothing to do rather than something was rejected.

SCOPE NOTE. Deliberately not folded into B-01KYN3E973F20 even though that work already had knowledge.go in its targets: the exit-code contract is unrelated to header round-tripping, and mixing them would have made the archive note describe two unrelated changes.

FIX DIRECTION. Per-entry refusals need to reach the exit status. Decide whether a partially-applied artifact is a failure (any refusal exits non-zero) or a partial success (exit non-zero only when nothing applied), then make the render say which entries were refused and why, rather than reporting only a count. VERIFY: a test asserting exit status and absence of an ok-shaped line for an artifact with one refused entry and one applicable entry.

## B-01KYSK7HQFFPM8538HAWGRS0P6 reconcileClosureBranch has no test coverage at all, and the records exemption now excuses real files at any depth rather than only at the root
kind: bug
state: done
created: 2026-07-30
grilled: 2026-08-02 open=3
targets: internal/workspace, internal/mcpserver, internal/wt

Two residuals surfaced by mutation verification of B-01KYSDBZTEF1A. Neither is a bypass and neither blocked that record; both are worth closing deliberately rather than by accident.

RESIDUAL 1, an untested reconcile path. reconcileClosureBranch has ZERO test coverage - a search for reconcile in gitflow_test.go returns no hits. B-01KYSDBZTEF1A changed a line inside it, replacing a root-anchored git add -A -- dot-spectackle pathspec with staging the exact files the conflict classifier resolved, because once the classifier became nesting-aware the old pathspec failed with pathspec did not match any files on a nested-only records conflict, leaving the file unmerged and aborting the merge. That fix was verified only at the git-mechanics level by a verifier own harness, plus by reading. It is pinned by nothing.

WHY IT IS HARD, and therefore why it is filed rather than done: the path runs during an online archive closure and wants a live forge, so a test needs either a fake forge or a seam that lets the reconcile run against a local repo with a real merge conflict. The second is probably the right shape and is a small refactor: the function already takes its git operations through a closure, so injecting a repo root and skipping the forge call would make the conflict-classify, checkout-theirs, stage, commit sequence testable in a temp repo. Measured behavior to pin once testable: a nested-only records conflict resolves and commits; an empty inside set makes git add with no pathspecs exit 0 and stage nothing, after which the commit correctly fails closed; a modify-delete or rename-rename records conflict fails at the preceding checkout-theirs in both the old and new forms.

ONE MORE MEASURED DIFFERENCE worth recording so nobody re-derives it: the old pathspec also swept an untracked or locally modified ROOT records file into the reconcile commit, which the new form leaves untracked. That is not a regression, because gitCommitRecords runs immediately afterward in gitFlowMerge and is nesting-aware, so the file lands one step later - but a test should assert that rather than trusting the sequencing.

RESIDUAL 2, the exempt surface widened from the root to every depth. workspace.IsRecordsPath exempts any path with a dot-spectackle SEGMENT, so a real file placed under any such directory at any depth - notes.txt, or a .go file - passes the scope gate and is then committed under the server records subject. Measured: three such paths pass at exit 0.

WHY IT WAS JUDGED ACCEPTABLE, and the caveat. The ROOT case behaved identically before the change, since the old expression exempted dot-spectackle slash anything too, so this widens an existing hole rather than opening one; and the Go toolchain ignores dot-directories entirely, confirmed by a verifier placing syntactically invalid Go under one without breaking go build. The caveat is real though: in a non-Go language a script or asset under a records directory IS reachable, and the surface is now every directory depth instead of one. So the exemption rests on a language-specific accident that the predicate does not state.

FIX DIRECTION. Exempt records by NAME rather than by directory: the server writes work.md, spec.md, journal.ndjson and its known siblings, so the gate could exempt those filenames inside a dot-spectackle segment and refuse anything else there. That keeps the deadlock fix - those are exactly the files that deadlocked - while removing the smuggling surface at every depth including the root, which is a strict improvement over the pre-existing behavior. VERIFY: a .go file and a shell script under a records dir at root and nested depth must both be REFUSED, while work.md and journal.ndjson at both depths are exempt.

## T-01KYT2EHRMEAHSAY9GECXG2035 sweep the remaining hard-coded enumerations onto their source of truth
kind: task
state: draft
created: 2026-07-30
targets: internal/mcpserver/commands.go, internal/mcpserver/tools.go, internal/mcpserver/prompts.go, internal/mcpserver/server.go

Found by the mutation verifier of B-01KYS7111XFHZ after that record fixed THREE hard-coded copies of the blocked-exit enumeration - two it set out to fix, and a third the verifier measured on the failing-verdict route, which is arguably the commoner way into blocked. The class has now recurred often enough in this codebase to be worth sweeping deliberately rather than one site at a time.

WHY IT MATTERS, measured rather than argued: a hard-coded enumeration is not merely duplication, it goes WRONG. override-once is one-shot, so a second escalation offers only rescope and reject, and every hard-coded copy kept advertising override-once - which the parser then refused at exit 1. A refusal exists to teach the value that will be accepted; naming one that will not costs the caller a whole call to discover.

REMAINING INSTANCES, all in non-test code, all reported with locations by the verifier. commands.go around line 227 spells claude-pipe-copilot-pipe-codex-pipe-kimi while validHarnesses at commands.go:68 is the actual set. tools.go around 1495 spells U-pipe-E-pipe-S-pipe-N-pipe-O-pipe-C while ears.patternNames at internal/ears/ears.go:30 is the actual set. Those two are the same shape as the fixed bug and should render from their maps.

SEPARATE JUDGEMENT NEEDED, not a mechanical fix: the static doc surfaces at prompts.go:86, tools.go:285 and server.go:81 also name all three blocked outcomes. Those are TRUE in general - the full set does exist - so they are not wrong the way a refusal is. But they sit awkwardly against the HINT-001 reasoning used to strip the same enumeration from nextAction: values belong on the refusal that rejects a wrong one, not on a line every session pays for. Decide it once for all three rather than per-site, and record the decision; they are manifest and tool-description text, so any change there is measurable on the schema and manifest lines the benchmark now meters.

DIRECTION for the mechanical two: render from the map, exactly as blockedExitOutcomes now does for the escalation surfaces. Add a test per enumeration asserting the rendered string equals the map contents, so adding a harness or an EARS pattern cannot leave a stale advertisement behind.

VERIFY per RECMERGE-003: for each site, mutate the underlying map - add a value - and assert the suite fails because the rendered enumeration no longer matches. That is the property, not the literal.

## T-01KYT90WD4FKX8K1AAE71X1DPD no test ever forces cd.Emit to fail, so the escalation broadcast-failure branch is unreachable coverage
kind: task
state: draft
created: 2026-07-30
targets: internal/mcpserver

Found by the callsite verifier of B-01KYS7111XFHZ as its own chosen mutation, and it is orthogonal to that record - the branch is untouched by it and was already correct.

MEASURED. There are two roundsRefusal call sites. Nil-ing the main one - the path every ordinary escalation takes - fails TestMoveEscalationAdvertisesLiveOutcomes. Nil-ing the OTHER one, which fires only when the coordination broadcast cd.Emit itself fails, fails nothing: grep finds zero hits for COORD E or escalate broadcast failed in any _test.go, so no test in the suite ever drives an Emit failure. A regression at that specific call site would ship silently.

WHY IT MATTERS beyond the one line. That branch exists for a deliberate reason worth preserving: the escalation HAPPENED and what failed is telling the siblings about it, so the LLM must hear about it rather than be told everything is fine. That is a correctness contract about honest reporting under partial failure, and it is currently asserted by nothing. Any refactor could delete or invert it undetected.

DIRECTION. Make the coordination emitter injectable in tests, or add a fault-injection seam on the Server, and drive one escalation with Emit forced to fail. Assert three things: the COORD E line is present and names the failure, the roundsRefusal block still follows it with the LIVE option set rather than a hard-coded list, and the item is genuinely blocked - the escalation must not be rolled back merely because the broadcast failed.

BROADER: this is likely not the only error branch with no test. Once a seam exists, sweep for other paths that only run when an infrastructure call fails - those are exactly the branches that carry the honest-reporting contracts this project cares about, and exactly the ones nothing exercises.

VERIFY per RECMERGE-003: with the test in place, nil-ing the option argument at that call site must fail the suite, and so must deleting the COORD E line.

## B-01KYTK0BGRF7X9PEY3QKWQ7TQA a rule added outside any record lifecycle has no edge to commit it, which invites bypassing the branch and PR flow
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/tools.go

OBSERVED on myself, which is the only reason it is worth filing: I added RECMERGE-004 with rule op=add while no record was active. The server wrote spec.md and made its own spectackle-paren-rule edge commit, and I then pushed that commit straight to main - bypassing the one-task one-branch one-PR flow this repository otherwise holds to. Nothing else rode along and main is clean, but the path was wrong and I reached for it because the alternative was unclear.

THE GAP. Every other write in this system is carried by a lifecycle edge: a move commits, pushes, opens or merges a PR, and the gates run. A standing rule is explicitly meant to be added the MOMENT a learning emerges - the workflow says so - which is frequently not while a record happens to be active. So the one write the process most wants to be immediate is the one write with no edge to carry it. The natural next step for an agent in that position is a manual git command, which is exactly the behavior the flow exists to prevent.

WHAT IS NOT THE FIX: telling agents to always have an active record first. That inverts the instruction to cast learnings immediately, and it would make a rule addition cost a full lifecycle - which is how learnings end up in agent-private notes instead, the failure the instruction is written against.

DIRECTION, and this needs a decision rather than a patch. Either a records-only write gets its own lightweight edge - commit, push, PR, gates - so the flow covers it, or rule op=add explicitly states that its write lands on the current branch and will be carried by the next edge, so the agent knows to leave it alone rather than improvise. The second is nearly free and may be sufficient; the first is more correct if rules should never sit uncommitted. Measure how long a pending rule typically waits for the next edge before choosing - if it is minutes, the cheap option wins.

VERIFY. Add a rule with no active record and confirm the surface tells the caller what happens to the write and what NOT to do about it. If the edge option is chosen instead, confirm the gates run on it and that a rule cannot reach main without them.
