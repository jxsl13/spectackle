---
schema: v0
---

## B-01KYD1G9RAEHWTK3SW3ZH3YFWS the stale-binary hint fires on released and packaged binaries, where its advice cannot be followed
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/swarm.go

GitHub issue 29. This is a defect in the MCP-010 hint shipped by T-0115, found by field use of the released binary rather than by the development checkout it was built and tested in.

OBSERVED: every tool call prepends the hint naming make dev, including on a freshly installed release binary in a repository that contains no Makefile at all. The advice is unfollowable for anyone who installed spectackle rather than building it. It fires on every tool without exception.

WHY IT IS WORTH FIXING RATHER THAN TOLERATING, per the reporter: it sits on the token path of a server whose stated purpose is long-term token efficiency, costing a fixed tax on every result while carrying no information for the majority of users; and it trains callers to filter h lines wholesale, which is the same record class used for real signal such as commands op=detect reporting a detected harness. A noisy channel gets filtered, and the useful records go with it.

ROOT CAUSE IN OUR OWN TERMS: the check compares the executable's modification time against the newest source file under the workspace root. In a development checkout that is meaningful. For an installed binary the sources under the user's own repository are almost always newer, so the condition is permanently true and says nothing. The feature was verified only from the perspective it was written in.

FIX DIRECTION: fire only where the advice is actionable — a development checkout of spectackle itself, where a rebuild is both possible and relevant. The staleness check already stats the executable, so it has enough information to recognize that it is not running from a development build; consider also that the version stamp distinguishes a tagged release from a dev build.

VERIFY: an installed binary in an unrelated repository emits no hint on any tool; a development checkout with sources newer than the binary still emits exactly one per crossing; the existing debounce and once-per-crossing tests keep passing.

## P-01KYD47GZ7FAMAGM4NEF0BQS8T turn review from assertion into evidence: run the gate that exists, then make grill compute what it cannot fake
kind: proposal
state: draft
created: 2026-07-25
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go

R-0007 completed: six lenses, 63 mechanisms, 52 naming a real failure. The second pass verified its predecessors against the live server and the code, and it overturns the first synthesis on its top-ranked detector.

FINDINGS VERIFIED INDEPENDENTLY BY THE ORCHESTRATOR, not taken on report:
- The submit gate has executed ZERO commands in this repository's entire history. config.yaml carried no verify key and git log over all history shows no goal field ever added to any item, so runGate built its command list from two empty sources and returned success. All seven submit events passed a gate that ran nothing. Confirmed by reading config.yaml and by counting additions of goal across all branches.
- The mechanism the first synthesis ranked second — a monoculture scan over the target package's test files — would NOT have caught B-0004. The literal main lives in internal/wt/wt.go line 298, inside InitTestRepo, which is production code in a different package; before T-0130's retrofit it appeared in none of internal/mcpserver's test files. Naive literal frequency is noise: op appears 102 times, id 84, while main is not in the top 50. Four lenses converged on a mechanism that does not work as specified, and the first synthesis promoted it on the strength of that agreement. Recorded as the finding it is: convergence across lenses is not verification.
- grill is ceremony in practice, not only in principle. Twelve grill events exist; every one fired between zero and ninety-one seconds after its item's own create event, three within the same second, and no item body was ever revised in response to one. P-0088 was grilled three minutes and twenty-five seconds BEFORE its child briefs existed, so the stamp authorizing approval preceded the material it was supposed to critique. The briefs section is structurally near-dead: in at least seven of twelve grills no task item existed yet.
- check cannot report a contract gap in this repository. The root bundle is unscoped and carries sixteen rules, so ForPath never returns empty and the coverage-gap branch is unreachable. Twelve of twenty-four packages under internal carry no bundle at all while SPX-REPO-002 mandates one, and check answers ok. Six of the ten dogfooded defects landed in exactly those uncovered packages.

FIRST ACTION TAKEN, because it needed no new machinery: verify commands are now configured, so the gate runs go build and go test on both sides of every submit. Proven to bite before enabling, in a scratch workspace with a deliberately failing command: the submit refused with a GATE error rather than merging. Measured cost of the real commands is about fifteen seconds per run. The field, its parser, its executor and its documentation all already existed and had simply never been switched on.

RANKED REMAINDER, by verified failures per unit of cost:
2. Server-computed environment differential at grill: print the live value of a fixed axis list beside what the item's tests construct. Four of ten defects in one section, roughly thirty lines of code. Its own limit is honest — the axis list is hindsight-fitted, and its real test is whether axis six gets added before defect eleven.
3. grill stamps a verdict bound to what it read, and move gates on that verdict rather than on a non-empty date. This is P-0060's adjudicated principle applied to the reviewer: a refresh is not a verification.
4. Package-local contract coverage: make the coverage gap fire per package instead of inheriting the root scope. Twelve violations are visible today. Weakest of the top five because an EARS sentence silences it; mitigate by requiring the silencing rule to bind a node via applies.
5. Blast radius and irreversibility computed from targets. T-0135 declared four files and landed fifteen; T-0137 rewrote journal history and passed the rollback word-check with a well-formed paragraph.
6. Declared-but-unconsumed sweep. B-0009's title is literally the finding.
7. Caller-divergence sweep: print only minority argument shapes among a callee's call sites. B-0003 was one against twenty.
8. Server-executed mutant-kill gate at submit. Strongest evidence generator after the first, ranked here only for its measured eighty-second tax on the package where most work happens.
9. Independent-oracle recall for recognizers, as a ratchet rather than a threshold. Would have caught R-0005 wholesale; also the only mechanism with a maintenance tail, and the property that carries it — the corpus is not the implementer's — is the one thing no lint can enforce.

TO DELETE RATHER THAN ADD: grill's word-presence questions and the brief substring heuristics. They cannot fail for a determined author, they train bodies to grow padding, and they occupy the slot where a computed check belongs.

Scope for the follow-up tasks is disjoint by file: grill.go for the env differential and the verdict stamp, tools.go and research.go for coverage, a new sweep for callers. Rollback for each is the removal of one section or one predicate.

## T-01KYD72GCXF998EDDG3BPKZT9W grill computes its critique and stamps a verdict; move refuses approval while computed gaps are open; the fakeable word-checks are deleted
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD47GZ7FAMAGM4NEF0BQS8T
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Create only what is specified; where a judgment call is left open it is named explicitly.

WHY. R-0007's organizing finding: every review mechanism splits into a server-computed half the author cannot fake and a written half they can, and grill today is almost entirely the written half. Twelve grill events exist in this repository; every one fired 0-91s after the item's create event and not one body was ever revised in response. The word-presence questions are satisfied by writing the word. This task makes grill compute what it can compute, stamp a verdict carrying the open-gap count, and lets move enforce that verdict. P-0060's adjudicated principle, applied to the reviewer: a refresh is not a verification.

VERIFIED GROUND (do not re-derive)
- grill.go:97 renders #questions via grillQuestions(it); :101 stamps it.Grilled with today's date unconditionally; :165 applies briefHeuristics to child-task bodies.
- briefHeuristics (grill.go:177): len<300 short-body, no "/" no-path, no "go test"/"make" no-verify. grillQuestions (grill.go:242): substring tests for scope/disjoint, rollback/revert, exit criterion/done when/verif; plus hasRecordedDeliberation for proposals (structural: checks refs and rejected alternatives - NOT fakeable, KEEP it).
- The move gate already exists: tools.go:1329-1338 - with feedback.grill=require a proposal with empty Grilled is refused; otherwise warned. Item.Grilled is a plain string; lifecycle keeps it on reopen (lifecycle.go:380) and replays it (Gr field).
- grillIn.Budget defaults to 1500 (grill.go:37); the pack is budget-truncated with a resume cursor.

WHAT TO BUILD
1. VERDICT STAMP. Grilled becomes "<date> open=<n>" where n counts only COMPUTED findings rendered in this grill pack (the classes below plus the existing notest records). Legacy bare-date stamps parse as open unknown and are treated as today (require refuses, non-require warns). Move to=approved under feedback.grill=require refuses while open>0, naming the count and classes in the refusal; without require it warns. Re-grilling after fixes re-stamps with the new count - that is the intended fix loop.
2. COMPUTED CLASSES, each replacing a deleted word-check, each one output line per finding:
   a. path-existence: every path-shaped token in the body (contains "/", matches an existing-file heuristic) that does not exist in the worktree -> "g nopath <token>". Replaces the no-path substring.
   b. verify-executability: lines in a VERIFY block that match a known-bad table -> "g badverify <pattern>". Seed the table with exactly two entries, each a recorded failure: lint -root (B-0010: the flag form silently lints nothing; positional is correct) and reading $? after a pipe (the false-confirmation method B-0010's rejection records). Table lives in grill.go as a small var; growing it is a one-line change. Replaces the no-verify substring.
   c. irreversibility-from-targets: targets matching journal.ndjson, coord.db, SchemaStamp/migration paths, or a target count >= 8 -> "g irreversible <target>" / "g blast <n> targets" demanding the body name a concrete restore path (check for a RESTORE or ROLLBACK section heading, not a word anywhere). Replaces the rollback substring.
   d. environment differential: a fixed axis list, one line each, live value computed from the workspace beside what the item's target packages' tests construct: "e <axis> live=<v> tests=<v|absent>". Axes, exactly five, each anchored to a recorded defect: primary-branch-name (B-0004: InitTestRepo hardcoded main), git-dir-shape file-vs-dir (B-01KYD1G9K), root-kind worktree-vs-checkout (B-0002), process-topology shared-vs-per-call (the lost-update defect B-01KYD57F), path-normalization case/sep (T-0136's d-bus finding). tests= side is a static scan of the target packages' _test.go files for a differing constructed value; absent when no test varies the axis. Absent counts as an open gap ONLY when the item's targets touch the axis's subsystem - a doc task must not be blocked on branch-name variance. State in the report how you scoped that condition per axis.
3. DELETE: the scope/rollback/exit-criterion substring questions and the short-body/no-path/no-verify heuristics. KEEP: hasRecordedDeliberation, grillTests (notest), grillRejections. The prose #questions section shrinks to the deliberation check only.
4. BUDGET. All new sections respect grillIn.Budget through the existing truncation path. The five env lines and the computed findings are emitted before lower-value sections so truncation never hides the verdict's inputs. The verdict line itself is exempt from truncation.

NON-NEGOTIABLE PROPERTIES, each with a test
- A proposal whose pack computed n>0 cannot reach approved under feedback.grill=require; the refusal names n. After fixing and re-grilling to open=0 the same move succeeds.
- A legacy bare-date stamp behaves as stated above (both config modes).
- Each computed class fires on a synthetic item constructed to trip exactly it, and stays silent on one that does not.
- The pack for an item with zero computed findings stamps open=0 and one grill call costs no more output bytes than before this change on the same item (measure both on P-0088 in this repository, report both byte counts).
- Word-presence padding no longer changes any outcome: a body containing the words scope, rollback, exit criterion but tripping class (c) still counts its gap.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root>   (POSITIONAL path - the -root flag form is entry one of your own known-bad table)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Red-run: before implementing the move gate, write its test and show it failing against current code; paste the failing output in the report.
CROSS-VERIFICATION (for the orchestrator, after done): an independent verifier re-runs the VERIFY block and the synthetic-item tests from the diff alone, without reading the implementer's report, and records agree/disagree in the archive note.

SCOPE: grill.go, tools.go move gate, their tests. Do not touch the item model (Grilled stays a string), lifecycle.go, templates, or prompts. tools.go is also named by T-01KYD2XQG6E38APSR3EY4GY137 (rule op=edit) - disjoint functions, but do not run concurrently with it; the lease on tools.go enforces that.
ROLLBACK: revert the commit; stamps written as "<date> open=<n>" remain parseable by the legacy path you keep for bare dates - state in the report that you verified a pre-revert stamp still loads post-revert.
REPORT BACK: where each class is computed, the per-axis scoping condition for (d), both byte measurements, each test's real result including the red-run, anything deliberately not done.

## T-01KYD72HNHEYAB0WF42BTR31CW research return path enforced at the archive gate: an R-item archives only consumed or explicitly closed
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD6VP6VE2Z8A517AT3RP39T
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/tools.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: the grill-verdict task (grill computes its critique and stamps a verdict) must be MERGED first - it restructures the move path in tools.go this task adds one gate to. Do not start while it is open. The package-coverage task also touches tools.go; whichever merges first, rebase on it - the regions are disjoint (move gate vs coverageGaps).

WHY. Research that changes nothing is pure token cost, and nothing today notices. R-0007 itself is the near-miss proving the class: its findings exist ONLY because the orchestrator chose to write P-01KYD47... - had the session ended first, six lenses and 63 mechanisms would have archived into a tombstone no rule, task or ADR cites, and the next session would re-pay the full price. The user requirement is explicit: research results must flow back into the application. This gate is the smallest mechanism that makes the return path mandatory rather than customary: one conditional at one call site, no sweeps, no background work.

VERIFIED GROUND (do not re-derive)
- The move path in tools.go (post grill-verdict restructure) is where to= transitions are validated; the grill gate at tools.go:1329-1338 is the pattern to mirror: compute, refuse with a dense record naming the reason, or warn depending on config strictness.
- Items carry Refs (draftIn.Refs; item model); rules carry rationale text; the FTS cache (s.cache.Search) indexes items and rules. Reverse-reference lookup = scan live items' Refs for the R-id (item.LoadAll, cheap) plus rules whose rationale cites it (cascade scan, in memory). Both are already-loaded structures - no new I/O.
- Archived items resolve as tombstones (LCY-001); a consumer that is itself already archived still counts - check the journal tombstone set too (lifecycle.Tombstone).

WHAT TO BUILD
1. At move to=archived (and the to=done shortcut when it implies archive) for kind=research: the item must have at least one consumer - a live or archived item whose Refs include the R-id, or a rule whose rationale names it - OR the move call's note must be non-empty and at least 80 characters (an explicit no-action closure stating why nothing changed). Otherwise refuse: "! BACKPROP E <id> unconsumed research - cite it from a rule/item or close with a no-action note".
2. The refusal is a hard error regardless of feedback config - unlike grill's require knob, an unconsumed-and-unexplained archive has no legitimate loose mode; if a future case needs one, that is a config discussion for then, not a default now. State this asymmetry in a code comment.
3. Reject stays untouched: move to=rejected already requires a note; a rejected R-item is a recorded dead end, which IS a return path.
4. No new output sections, no sweeps over archived history at check time - the gate fires only at the moment of archiving, cost O(live items + rules), both already in memory.

NON-NEGOTIABLE PROPERTIES, each with a test
- An R-item with zero consumers and no note refuses with the exact record; the same item with an 80+ char note archives; the same item cited by one task's Refs archives without a note.
- A consumer that is itself archived (tombstone) counts - fixture: archive the consumer first, then the R-item.
- A rule whose rationale cites the R-id counts - fixture through the rule op=add path.
- Non-research kinds are untouched: a task with no consumers archives exactly as today (existing tests unmodified).
- The gate's cost is flat: no file reads beyond the already-loaded work.md/journal/cascade (assert no new Open/ReadFile calls in the gate path - review the diff, state it in the report).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Red-run: write the refusal test first, show it failing against current code, paste the failing output.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the four fixtures from the diff alone; verdict recorded in the archive note.

SCOPE: the move gate region of tools.go plus tests. Do not touch grill.go, lifecycle.go's state machine, the item model, or templates (the sibling task documents this gate; you implement it).
ROLLBACK: revert the commit - the gate is one conditional; no stored state, no format change.
REPORT BACK: where the gate landed, the consumer-lookup implementation, each fixture's real result including the red-run, anything deliberately not done.
