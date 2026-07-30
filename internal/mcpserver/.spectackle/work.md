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

## B-01KYRVXQ02FDH9YBAFG64SH13N knowledge export mixes the artifact and its ok line on one stream, so export piped into apply fails with an unmappable line number
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/knowledge.go

Found by an independent verifier while checking something else, and it cost that verifier a wrong hypothesis before it identified the cause - which is the real damage here.

OBSERVED. knowledge op=export writes the artifact to stdout AND appends its own ok export record line to the same stream. Piping that stdout into op=apply body= therefore feeds the record line to the YAML parser, which fails with: yaml: line 17: could not find expected colon. The line number is ENTRY-RELATIVE, so it points at a coordinate the caller cannot locate in what it sent - the caller counts lines in its own input and finds nothing wrong at line 17.

EXPECTED. Either the artifact is the only thing on stdout so the obvious pipe works, or the refusal explains that the input carries a trailing record line and names it. The documented path= route works in both directions, so this is not a broken feature - it is a shape that invites a wrong call and then misdirects the caller who makes it.

WHY IT MATTERS FOR TOKEN ECONOMY. An agent that pipes export into apply gets a parser error naming a line it cannot find, and the cheapest recovery is to re-read both artifacts and the tool docs. The error is worse than no error, because it sends the reader to the wrong place. This is the same failure class as a refusal that renders a success-shaped line: the output describes a world the caller is not in.

FIX DIRECTION. Decide which stream owns the record line. Cleanest is that an op whose output IS a document emits only the document on stdout and its ok line on stderr, matching how a caller would naturally compose it. If the two must share a stream, the apply parser should detect a trailing record line and say so by name instead of surfacing a raw YAML error, and any line number it reports must be in the coordinate system of the input the caller supplied.

VERIFY. A test that pipes export output directly into apply and asserts it either succeeds or refuses with a message naming the trailing record line; plus an assertion that any reported line number resolves against the caller input.

## B-01KYSK7HQFFPM8538HAWGRS0P6 reconcileClosureBranch has no test coverage at all, and the records exemption now excuses real files at any depth rather than only at the root
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/gitflow.go

Two residuals surfaced by mutation verification of B-01KYSDBZTEF1A. Neither is a bypass and neither blocked that record; both are worth closing deliberately rather than by accident.

RESIDUAL 1, an untested reconcile path. reconcileClosureBranch has ZERO test coverage - a search for reconcile in gitflow_test.go returns no hits. B-01KYSDBZTEF1A changed a line inside it, replacing a root-anchored git add -A -- dot-spectackle pathspec with staging the exact files the conflict classifier resolved, because once the classifier became nesting-aware the old pathspec failed with pathspec did not match any files on a nested-only records conflict, leaving the file unmerged and aborting the merge. That fix was verified only at the git-mechanics level by a verifier own harness, plus by reading. It is pinned by nothing.

WHY IT IS HARD, and therefore why it is filed rather than done: the path runs during an online archive closure and wants a live forge, so a test needs either a fake forge or a seam that lets the reconcile run against a local repo with a real merge conflict. The second is probably the right shape and is a small refactor: the function already takes its git operations through a closure, so injecting a repo root and skipping the forge call would make the conflict-classify, checkout-theirs, stage, commit sequence testable in a temp repo. Measured behavior to pin once testable: a nested-only records conflict resolves and commits; an empty inside set makes git add with no pathspecs exit 0 and stage nothing, after which the commit correctly fails closed; a modify-delete or rename-rename records conflict fails at the preceding checkout-theirs in both the old and new forms.

ONE MORE MEASURED DIFFERENCE worth recording so nobody re-derives it: the old pathspec also swept an untracked or locally modified ROOT records file into the reconcile commit, which the new form leaves untracked. That is not a regression, because gitCommitRecords runs immediately afterward in gitFlowMerge and is nesting-aware, so the file lands one step later - but a test should assert that rather than trusting the sequencing.

RESIDUAL 2, the exempt surface widened from the root to every depth. workspace.IsRecordsPath exempts any path with a dot-spectackle SEGMENT, so a real file placed under any such directory at any depth - notes.txt, or a .go file - passes the scope gate and is then committed under the server records subject. Measured: three such paths pass at exit 0.

WHY IT WAS JUDGED ACCEPTABLE, and the caveat. The ROOT case behaved identically before the change, since the old expression exempted dot-spectackle slash anything too, so this widens an existing hole rather than opening one; and the Go toolchain ignores dot-directories entirely, confirmed by a verifier placing syntactically invalid Go under one without breaking go build. The caveat is real though: in a non-Go language a script or asset under a records directory IS reachable, and the surface is now every directory depth instead of one. So the exemption rests on a language-specific accident that the predicate does not state.

FIX DIRECTION. Exempt records by NAME rather than by directory: the server writes work.md, spec.md, journal.ndjson and its known siblings, so the gate could exempt those filenames inside a dot-spectackle segment and refuse anything else there. That keeps the deadlock fix - those are exactly the files that deadlocked - while removing the smuggling surface at every depth including the root, which is a strict improvement over the pre-existing behavior. VERIFY: a .go file and a shell script under a records dir at root and nested depth must both be REFUSED, while work.md and journal.ndjson at both depths are exempt.

## B-01KYSX35RKFYBRX6YAB9E9DHBW the needs bookkeeping is inverted: resolving paths keep the spent ADR while the non-resolving path clears it
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/decide.go

Split out of B-01KYS7111XFHZ rather than folded into it, because it needs its own reproduction and that record was already carrying a severe unrecoverable-state fix.

OBSERVED, from reading resolveDecision while fixing the escalation validation. A blocked item whose choice is one of rescope, reject or override-once resolves via lifecycle.ResolveBlocked - and those paths leave the now-spent ADR in the item Needs. Any other blocked-on item takes the else branch, which CLEARS this ADR from Needs without resolving anything. That is backwards on both sides: the path that consumed the decision keeps the link, and the path that consumed nothing drops it.

WHY IT WAS NOT FIXED IN THE SAME RECORD. The severe consequence - a typo burning the decision, clearing the link and stranding the item forever - is closed, because an invalid choose can no longer reach resolveDecision at all: the enumeration is validated first now. So the inversion is no longer reachable via the typo path. It remains reachable by any OTHER caller that lands in the else branch with a legitimately non-blocking decision, and the record for that fix should show which callers those are rather than assuming.

WHAT TO ESTABLISH FIRST, before changing anything. Enumerate every path into resolveDecision and say, for each, whether the ADR was consumed. The decide-ask-on-any-item case is the documented non-blocking one - a decision that names an item via decide op=ask item=X appends to that item Needs but never puts it in blocked - so clearing on answer may be correct THERE and the defect may be narrower than it looks from the branch shape alone. Do not refactor on the shape; measure which callers reach which branch.

DIRECTION, if the reading holds. Needs should be cleared exactly when the decision that occupied it has been answered, regardless of whether the answer also unblocked a state transition - clearing is about the link being spent, not about what the outcome did. Then ResolveBlocked and the else branch converge on one rule instead of two opposite ones.

VERIFY. A test per caller path asserting the post-answer Needs contents, including a blocked item resolved by each of the three outcomes and a non-blocking decide-ask-on-item decision. Per RECMERGE-003, mutate: swapping the two branches must fail the suite, which today it would not, since nothing asserts Needs after resolution.

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
