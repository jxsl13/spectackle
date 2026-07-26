---
schema: v1
---

## B-01KYD1G9J5EHBBT823EK0MGT3T indexer walks gitignored paths, so vendored and virtualenv copies inflate the graph and steal the unsuffixed node ID
kind: bug
state: draft
created: 2026-07-25
targets: internal/index/indexer.go, internal/workspace/workspace.go

GitHub issue 26. Reported from field use on a real working checkout.

OBSERVED: the walk descends into gitignored directories. One real symbol yields three nodes, one per copy. The ranking is the damaging part: the copy inside .venv sorts FIRST and receives the unsuffixed node ID, so an agent anchoring a rule via applies to the top-ranked ID pins its contract to a file inside a virtualenv.

FIELD MEASUREMENT: same commit, same code — a clean checkout indexes 1809 files, the long-lived working checkout indexes 24083. Thirteen times the index, from a gitignored 1.2 GB virtualenv and a 1.2 GB model directory.

WHY IT MATTERS BEYOND SIZE: it works directly against the token-economy premise the server advertises, since the result set is inflated with copies the repository itself declares irrelevant.

SCOPE NOTE FROM THE REPORTER, worth trusting: registered git worktrees are ALREADY skipped correctly in every position tested, so the gap is specifically gitignore, not extra directories in general. The existing ignore and ignore_regex config knobs do let an operator patch this per repository, but that inverts the default — every new checkout gets a polluted graph until someone enumerates their own ignore list a second time.

FIX DIRECTION: honor gitignore during the walk. Decide how: parsing gitignore semantics correctly is more than a prefix match (negation, directory-only patterns, nested files), so consider asking git itself, and weigh that against the cost of a subprocess per index and the behavior in a non-git workspace, which must keep working.

VERIFY: the reproduction yields exactly one node for the real symbol, with the real source file's path; a non-git workspace still indexes everything; a repository with negated gitignore patterns behaves as git does.

## B-01KYD1G9KQF87REB16T0AXRDYP workspace resolution walks past .git files, so a nested worktree can never be the root and writes land in the parent checkout
kind: bug
state: draft
created: 2026-07-25
targets: internal/workspace/workspace.go

GitHub issue 27. Reported from field use with an agent harness that places worktrees inside the repository.

OBSERVED: root resolution walks up from the given start to the nearest ancestor containing a .git DIRECTORY, skipping .git FILES. A git worktree nested inside its main checkout therefore never becomes the workspace root, not even with an absolute -root naming it. The bundle is written to the enclosing checkout, and the reported path is printed relative to the resolved root, which hides where the write actually went — the answer reads as if it landed locally.

THE REPORTER PINNED THE RULE with three probes rather than guessing: -root pointing at a different repository does target that repository, so the flag works; with no git anywhere above, the workspace anchors at the given directory rather than walking to the filesystem root; and a worktree placed OUTSIDE the main checkout does get its own bundle. The discriminator is exactly whether a .git directory exists above.

WHY IT MATTERS: several coding harnesses place agent worktrees inside the repository. Under this behavior every such agent writes to the shared main checkout's bundle regardless of what it passes, which is precisely the collision the swarm and lease design exists to prevent.

FIX DIRECTION: a .git file marks a worktree root and should terminate the upward walk exactly as a .git directory does. Note the tension the reporter names: centralizing the bundle at the main checkout may be deliberate, since work op=start manages worktrees itself and the worktrees directory defaults inside the repository. If so the bug is narrower but still real — an explicit -root must not silently resolve elsewhere, and the reported path must be unambiguous about which root it is relative to. Decide and record which reading is correct.

VERIFY: the reproduction writes into the worktree's own bundle; a worktree outside the checkout keeps working; work op=start's own worktrees continue to resolve as they do today.

## B-01KYD1G9PSEH5AQHAV7N4ZQ4BT a degraded index is invisible through the MCP surface: state answers ok graph after the typed-call pass fails
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/state.go, internal/index/typespass.go

GitHub issue 28. Reported from field use where the target repository's Go version exceeded the toolchain the released binary was built with.

OBSERVED: the typed-call pass failed on 72 packages and the graph silently degraded to syntactic-only. The only notice is a line on reindex's stderr, which an agent driving the server through the call subcommand or over stdio and HTTP never sees. Through the tool surface, state answers ok graph with node and edge counts and no mention of the degradation. check likewise reports nothing.

WHY IT IS DANGEROUS RATHER THAN UNTIDY: the degradation removes exactly the capability the instructions advertise as the reason to prefer get depth over shell search — cross-language impact radius, and what calls X. With the typed pass gone there are no typed call edges at all, so an impact query returns a confident-looking but structurally incomplete answer. The tool still responds, still says ok, and the caller has no signal to distrust the radius. An agent consulting get depth=2 before editing a symbol underestimates blast radius and never learns why.

FIX DIRECTION, two independent closures, either of which suffices and both of which are cheap: propagate the index-degradation state into state and check as a first-class record an agent can branch on, naming the cause and the affected package count; and have reindex report the toolchain mismatch as an actionable diagnostic, since it already prints both versions and rebuilding with a newer Go is a fix the operator can act on immediately.

VERIFY: with a forced typed-pass failure, state emits a degradation record rather than a bare ok; the record names the cause; a healthy index emits nothing extra, so the output diet is preserved.

## B-01KYD57FN3ERHBM5EQ3534YJXP concurrent draft from two agents silently loses items: work.md is read-modify-written with no cross-process lock
kind: bug
state: draft
created: 2026-07-25
targets: internal/item/item.go, internal/mcpserver/tools.go

MEASURED, not inferred. Surfaced by strengthening TestTwoServersMintUniqueIDs to assert on the IDs stored on disk instead of the IDs the servers printed.

REPRODUCTION: two Server instances on one root (the twoAgents helper), 8 concurrent draft calls each, 16 total. Every call returns ok with a distinct full ID. Fewer than 16 items are on disk afterwards. Observed 15 of 16 repeatedly and 10 of 16 once; a 20-run probe lost records in 2 runs, worst case 1 of 16. The rate is load dependent, so a quiet workspace hides it and a busy swarm does not.

MECHANISM: item.Upsert is an unsynchronized read-modify-write of the whole file. It calls LoadWork to read every item, splices its own item into the slice, and writes the entire file back via writeWork. Two concurrent Upserts both read N items and both write N+1, so the later write erases the earlier writer's item. The only mutex in the path is Server.mu, which is per process; the shipped topology is N stdio processes, so it provides no mutual exclusion at all. Two goroutines inside one process are merely the cheapest way to reproduce what N processes do by default.

WHY THIS IS NOT COVERED BY P-0088: that proposal closes ID collision, and it does. This is a lost update, and unique IDs do not help - the second writer clobbers a record whose ID was never in doubt. Both defects had to be fixed for concurrent drafting to work; only one was.

WHY IT WAS INVISIBLE: the old test compared the printed short IDs, and check only reports duplicate IDs (E101). Neither can see a record that is simply absent. Nothing in the suite asserted a post-condition count.

SEVERITY: silent data loss on the primary write path, no error returned to the caller. The draft is acknowledged with an ID that resolves to nothing.

FIX DIRECTION (needs a decision, do not implement blind): the write must be serialized across processes. Candidates are an advisory file lock around the read-modify-write, or routing item writes through coord.db, which already exists as the cross-process serialization point and already holds leases. The second is more invasive but removes a second lock hierarchy. Remove is the same shape and has the same hole; check both. Whatever is chosen, the regression test is the reproduction above asserting an exact post-condition count.

KNOWN-FAILING TEST: TestConcurrentDraftsPersistEveryItem in internal/mcpserver/swarm_test.go, skipped with a pointer to this item. Unskipping it is the acceptance test.

## P-01KYD87FJREJ5SD0G2RDCMZ32Y turn review from assertion into evidence: run the gate that exists, then make grill and validation compute what they cannot fake
kind: proposal
state: submitted
created: 2026-07-25
refs: R-0007, P-01KYD47GZ7FAMAGM4NEF0BQS8T
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/evidence

Supersedes the draft it cites in refs, which carried two errors its own validation round caught; corrected here, everything else re-recorded intact.

R-0007 completed: six lenses planned, four reported before the session limit killed the rest, 40 mechanisms proposed, 34 naming a real failure from this repository's history. The second pass verified its predecessors against the live server and the code, and it overturned the first synthesis on its top-ranked detector.

FINDINGS VERIFIED INDEPENDENTLY BY THE ORCHESTRATOR, not taken on report:
- The submit gate executed ZERO commands in this repository's entire history: config.yaml carried no verify key and no goal field was ever added to any item, so runGate built its command list from two empty sources and returned success on all seven submits. Fixed first (verify commands armed, proven to bite in a scratch workspace before enabling; measured cost about fifteen seconds per run).
- The mechanism the first synthesis ranked second - a monoculture scan over the target package's test files - would NOT have caught B-0004: the literal main lives in internal/wt/wt.go:298 inside InitTestRepo, production code in a different package; naive literal frequency is noise (op 102 hits, id 84, main not in the top 50). Four lenses converged on a mechanism that does not work as specified. Convergence across lenses is not verification.
- grill is ceremony in practice: twelve grill events, every one 0-91s after its item's create event, three within the same second, zero bodies ever revised in response. P-0088 was grilled 3m25s BEFORE its child briefs existed.
- check cannot report a contract gap here: the root bundle is unscoped with sixteen rules, so ForPath never returns empty and the coverage branch is unreachable. ELEVEN of twenty-four packages under internal/ carry no bundle (thirteen do) while SPX-REPO-002 mandates one - the prior draft said twelve, recounted by the independence validator at eleven. Six of the ten dogfooded defects landed in uncovered packages.

RANKED REMAINDER, by verified failures per unit of cost (1 = the armed gate, done):
2. Server-computed environment differential at grill: live values of a fixed axis list beside what the item's tests construct. Four of ten defects in one section, roughly thirty lines.
3. grill stamps a verdict bound to what it read; move gates on the verdict, not a non-empty date. P-0060's adjudicated principle applied to the reviewer.
4. Package-local contract coverage with the applies-binding mitigation (a lazily written root sentence with no applies binding silences nothing). Eleven violations visible today.
5. Blast radius and irreversibility. CORRECTED SCOPE from the anti-ceremony validation: at grill time, on declared targets, this is a TRIPWIRE only - T-0137 gamed the word-check with a well-formed paragraph and a heading check is gameable the same way, and T-0135's 4-declared/15-landed divergence is invisible pre-implementation by construction. Declared-vs-landed belongs to the post-implementation validation phase's diff computation, where it is computable exactly.
6. Declared-but-unconsumed sweep (B-0009's title is the finding).
7. Caller-divergence sweep, minority argument shapes (B-0003 was one against twenty).
8. Server-executed mutant-kill gate at submit - strongest evidence generator after the first, deferred on its measured eighty-second tax until the anti-ceremony lens re-runs against real measured costs of 2-7.
9. Independent-oracle recall for recognizers, as a ratchet - would have caught R-0005 wholesale; the only mechanism with a maintenance tail; deferred with 8.

TO DELETE RATHER THAN ADD: grill's word-presence questions and the brief substring heuristics - including the substring half of the deliberation check itself (strings.Contains on the word rejected, found by this set's own validation round). They cannot fail for a determined author, they train bodies to grow padding, and they occupy the slot where a computed check belongs.

CHILD TASKS: the grill-verdict and validation-phase tasks live under the review-and-validation proposal (verdict machinery, identity binding); under THIS proposal: package-local coverage and the evidence sweeps. Scope is disjoint by file; rollback for each is the removal of one section or one predicate.

## P-01KYD87FX0F6YRX49R3A8TB6E4 backpropagation: every loop result flows back into the workspace, and the server names the next step so no step can be silently skipped
kind: proposal
state: submitted
created: 2026-07-25
refs: R-0007, P-01KYD6VP6VE2Z8A517AT3RP39T
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, internal/mcpserver/tools.go, docs/agent-workflow.md

Supersedes the draft it cites in refs: that draft's scope note carried a dangling task ID the independence validator proved resolves to not-found, and its note-requirement remedy contradicted its own no-written-signals standard; both corrected here, everything else re-recorded intact.

PROBLEM. The loop's forward path is well defined: research, draft, grill, approve, implement, check, archive. The backward path - how results change the workspace so the next iteration is smarter - exists only as convention. Three symptoms, each verified in this repository: (1) the server's own backprop concept covers exactly one flow, code-to-spec drift (check fix=true drafts one proposal per drifted rule, tools.go:1733) - research results, implementation reports and rejections have no defined return path; (2) the workflow template's final step says archive and commit but not what must be captured; the archive note is the training signal - it becomes the journal tombstone and the FTS body future sessions search - yet nothing says so and an empty note passes; (3) the template omits the post-merge restart entirely: CONTRIBUTING.md mandates make dev because the resident server IS the product under change, and the machine-facing instructions never mention it - real stale-binary confusion resulted this session.

WHY IT MATTERS FOR TOKEN COST. Knowledge that does not land in one of the three durable stores (spec.md rules, journal tombstones with substantive notes, knowledge artifacts) is re-derived by a later session at full exploration price. The backward path is the token-saving mechanism, not an overhead on it.

ENFORCEMENT LAYERING, stated once for all note requirements in this set: prose reminders in templates are guidance; length floors are tripwires against accidental emptiness, gameable by padding and known to be; SUBSTANCE is enforced only by hard gates bound to computed facts - the research-consumption gate (child task here) and the validation verdict gating archive for task and bug kinds (the validation-phase task under the review-and-validation proposal). A note requirement that is only prose is listed as guidance, never claimed as a control.

DELTA. Two child tasks:
1. Define the loop's backward edges in every machine-facing surface (server instructions, workflow template, next-step prompt) so each state names its one next action and each completed item states where its learning landed. Bounded: hints are one line, computed from actual state.
2. Enforce the research return path at the one gate that can see it: archiving an R-item requires either a consumer (a live or archived item or rule citing it) or an explicit no-action note. One conditional at one call site.

EXPLICITLY REJECTED: a generic workflow engine; any always-on background process; LLM-written self-assessments as evidence.

EXIT CRITERION. A fresh orchestrator session driven only by the server's own prompts performs research capture, archive notes, and post-merge restart without any of them being in its own system prompt - measured by driving the loop once headlessly and checking the three stores gained the expected records.

ROLLBACK. Each surface change is a template/instruction edit; the R-item gate is one conditional. Reverting the commit restores the prior loop; no data format changes.

SCOPE DISJOINTNESS. Task 1 touches server.go/prompts.go/templates/docs. Task 2 touches the move path in tools.go, which the grill-verdict task under the review-and-validation proposal (title: grill computes its critique and stamps a verdict) restructures first - task 2 declares NEEDS on that task BY TITLE and runs after it merges. No task ID is cited here by prefix guess; the prior draft's lesson is recorded: reference sibling work by exact title phrase or full minted ID, never by a predicted prefix.

## T-01KYD87ZN7EJ49CMSEQE9XGGWS package-local contract coverage: silent by default with visibility in state, counted by check only under coverage_gate
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD87FJREJ5SD0G2RDCMZ32Y
refs: R-0007, T-01KYD72GQ6E2ZV0HX8S443NPY6
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/mcpserver/state.go, internal/workspace/workspace.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs, whose central output design was proven impossible against the real code by this set's anti-ceremony validation; the corrected design is below and the impossibility is part of your ground truth.

NEEDS: the grill-verdict task (title: grill computes its critique and stamps a verdict) must be MERGED first - it restructures tools.go's move path and this task edits tools.go's check path; the lease serializes regardless. ALSO DISCLOSED: T-01KYD2XQG6E38APSR3EY4GY137 (rule op=edit recomposition) is an open draft targeting internal/spec/author.go AND internal/mcpserver/tools.go - if it becomes active before you, coordinate through the lease and rebase on whichever merges first.

WHY. check cannot report a contract gap in this repository: the root bundle is unscoped and carries 16 rules, so spec.Cascade.ForPath never returns empty and the coverage branch (tools.go:1695-1716, fires on len(ForPath(rel))==0) is structurally unreachable. ELEVEN of twenty-four packages under internal/ carry no bundle (thirteen do - counted by this set's independence validator; the prior draft said twelve) while SPX-REPO-002 mandates one, and check answers ok. Six of the ten dogfooded defects landed in uncovered packages.

THE OUTPUT-CHANNEL CONSTRAINT, verified against the real code - this is why the prior draft was impossible: check() has exactly ONE path that renders ok: if len(lines) == 0 return text("ok") (tools.go:1679-1680); any non-empty lines returns budget.Render(kept, cur) verbatim with NO trailing ok (budget.go:68-76 is a plain newline join; text() at tools.go:147-148 is a pass-through). The CI self-hosting gate does FULL-STRING equality: result != "ok" exits 1 (ci.yml:71-76). Therefore ANY unconditional visible output from check - twenty lines or one summary line - turns this repository's own CI red on merge. Visibility-without-gating cannot live in check's output channel. Do not rediscover this; design around it as specified.

WHAT TO BUILD
1. COVERED(pkg): a source dir under internal/ or cmd/ is covered iff (a) a non-root bundle exists at it or an ancestor below the root, or (b) at least one root-bundle rule binds a node inside it via applies (resolve applies targets to paths through the anchors table; a rule with empty applies never covers anything outside its own dir). This is the mitigation: a lazily written root-level EARS sentence with no applies binding silences nothing. Cost: O(rules x anchor rows), both already in memory - no new I/O; state this holds in a code comment.
2. DEFAULT VISIBILITY lives in state, not check: state's #rules section already renders one line per dir (ok dir <d> rules=<n>); append the token uncovered to dirs failing COVERED. state is not string-matched by CI - VERIFY includes proving that (read ci.yml; only check's output is compared to ok). Zero new lines, one token appended to existing lines - no output growth beyond 10 bytes per uncovered dir.
3. GATING: workspace config key coverage_gate: package (FeedbackCfg sibling or top-level key - pick, justify) makes check emit g nocontract <dir> lines (sorted, capped 20 + "+<n> more" tail) that COUNT as findings - CI red until backfilled, by explicit opt-in only. Default absent: check emits NOTHING for coverage - identical output to today, byte for byte, proven by a test.
4. This repository does NOT set the key in this task. The report lists the eleven dirs as the backfill worklist.

NON-NEGOTIABLE PROPERTIES, each with a test
- Byte-identity default: on a workspace with uncovered dirs and no key, check output is byte-identical to pre-change (golden test).
- state marks exactly the uncovered dirs; adding one applies-bound root rule into pkg X removes exactly X's token.
- With coverage_gate: package, check emits the capped records and does NOT end ok; without, it does.
- Cap holds: 40 uncovered dirs -> 20 lines + tail.
- Unknown-key tolerance: a workspace that sets the key loads on a server built without this change (YAML ignores unknown keys - verify, state in report).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok AND is byte-identical to a pre-change run on the same tree (paste both, diff empty)
  spectackle call -root <worktree-root> state '{}' - paste the #rules section showing the eleven uncovered tokens.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the golden byte-identity test and the gated-mode test from the diff alone; verdict recorded in the archive note.

SCOPE: coverageGaps and the check wiring in tools.go, the state.go rules-section token, the config key in workspace.go, tests. Do not touch grill.go, the spec package, or the anchors format.
ROLLBACK: revert the commit; the config key is additive and ignored by older servers.
REPORT BACK: the COVERED implementation, both pasted check runs with empty diff, the state section, the eleven-dir worklist, each test's result, anything deliberately not done.

## T-01KYD88KEDEAQ97QKQ46DSGTM4 evidence sweeps scoped to an item's targets: declared-but-unconsumed symbols and minority call shapes, with explicit per-symbol suppression
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD87FJREJ5SD0G2RDCMZ32Y
refs: R-0007, T-01KYD72H15EPV8KCW6ASSMEFZX
grilled: 2026-07-25
targets: internal/evidence, internal/mcpserver/grill.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs; the one change is the suppression mechanism (its absence let an acknowledged false-positive class gate approval with no escape - anti-ceremony validation finding); everything else is re-recorded intact.

NEEDS: the grill-verdict task (title: grill computes its critique and stamps a verdict) must be MERGED first - this task adds sections to the pack that task restructures, and both touch grill.go. Do not start while it is open.

WHY. Two defect classes from this repository's history are visible statically at review time, scoped to an item's targets: B-0009 (a schema column declared, never written or read) and B-0003 (workAbort passed an item ID where twenty sibling call sites passed a directory - one against twenty). Both sweeps run only over declared targets; a global sweep was considered and rejected for unbounded output.

VERIFIED GROUND (do not re-derive)
- graph.Edge is {Src, Dst, Kind, File, Line} (graph.go:137-142) - NO argument metadata. Caller-divergence therefore re-parses the call sites' files (go/ast), bounded to files the graph's inbound ECall edges name. Non-Go targets skip with "e skipped <node> non-go".
- The unconsumed sweep comes from the stored graph: exported symbols under a target path with zero inbound ECall/EUse edges from outside their own file. Known false-positive class: reflection/plugin lookup - which is exactly why suppression exists.
- grill's sections and budget truncation: per the NEEDS task. New sections render before #rejections, after the computed classes.

WHAT TO BUILD
1. Package internal/evidence: Unconsumed(g, targets, suppressed) []Record and DivergentCallers(g, targets, load func(path) []byte) []Record. Deterministic order, one line per Record, both capped at 10 + "+<n> more" tail.
2. SUPPRESSION, the corrected part: an item body may carry lines of the form unconsumed-ok: <symbol> <one-line reason>. The sweep consumes them: a suppressed symbol renders as "e suppressed <symbol> <reason>" (informational, never counted, so the waiver is visible in the pack rather than silent). An unconsumed-ok line naming a symbol the sweep did not flag renders "e stale-suppress <symbol>" - suppressions must not outlive their reason. This mirrors the applies-binding escape in the coverage task: the escape is explicit, recorded in the reviewed artifact, and per-symbol - never a blanket toggle.
3. Divergence definition: for each callee node under targets with >= 5 inbound call edges, group call sites by (argument count, per-argument shape class: literal-kind, identifier, call-result, selector). Report groups with share <= 20% as "e divergent <callee> <k>/<n> sites differ: <file:line>..." (first 3 sites). Thresholds are consts with one-line rationales; B-0003's 1-of-21 must trip, a 50/50 split must not.
4. Wire into grill as #evidence, subject to the pack budget. Counting into the verdict's open tally: UNSUPPRESSED unconsumed records count only when the item's kind is task or bug; divergent records are always informational (they may be the point of the change); suppressed and stale-suppress never count.
5. Cost ceiling, enforced: the AST pass parses only files the graph names as call sites of target callees; a guard refuses more than 50 files and reports "e truncated ast >50 files". SPX-MCP-001 (1 MiB reads, 2s warm) applies - add the evidence pass to the timing test and report measured wall time on this repository with targets internal/mcpserver.

NON-NEGOTIABLE PROPERTIES, each with a test
- B-0009 fixture (exported symbol, zero inbound) is reported; adding one consumer removes it; adding unconsumed-ok for it moves it to suppressed and out of the counted tally; removing the symbol while keeping the directive yields stale-suppress.
- B-0003 fixture (21 sites, 1 divergent) reports exactly the divergent site; a 10/10 split reports nothing.
- Caps hold: 30 unconsumed / 30 divergent -> 10 + tail each.
- Determinism: two runs on one fixture are byte-identical.
- Non-Go targets skip cleanly.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Run grill on a real item in the worktree with targets internal/mcpserver; paste the #evidence section and its wall time.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the two fixture tests including the suppression arms and the real-item grill from the diff alone; verdict recorded in the archive note.

SCOPE: internal/evidence (new), the grill wiring, tests. Do not modify internal/graph (arg metadata on edges was considered and rejected: it grows every edge for one consumer), the index, or the item model.
ROLLBACK: remove the #evidence section call; the package is dead code until deleted. No stored state.
REPORT BACK: measured wall time and bytes on the internal/mcpserver run, both fixtures' outputs including suppression, thresholds as landed, each test's result, anything deliberately not done.

## T-01KYD88KV5EX2SBYE81TKYHDH9 the backward path in every machine-facing surface: state-computed next steps, archive notes as the training signal, post-merge restart in the loop
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD87FX0F6YRX49R3A8TB6E4
refs: R-0007, T-01KYD72HB0FHX9G80DQGS9YBB1
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs. Its validation round found: a wrong rule pointer stated twice (SPX-MCP-006 governs the research tool; the rule that pins TOKEN ECONOMY is MCP-006), a substance test left as prose, and two unbounded/unmeasured caps. All corrected below; everything else re-recorded intact.

WHY. The loop's backward path is convention, not definition. An LLM driving the loop from the server's own surfaces is never told: that the archive note becomes the journal tombstone and the FTS body every later session searches (the note IS the training signal); that research results must land as rules, ADRs, tasks or an explicit no-action note; that after every merge the resident server must be rebuilt (CONTRIBUTING.md mandates make dev; the machine-facing surfaces never mention it - real stale-binary confusion this session); or what the single next action is for the state an item is in. Each omission costs a later session full re-derivation price.

ENFORCEMENT LAYERING (from the parent proposal, restated because it bounds this task): everything this task ships is GUIDANCE surfaces plus computed next-step hints. The hard gates for note substance live elsewhere: the research-consumption gate (sibling task) and the validation verdict gating archive for task/bug kinds (the validation-phase task under the review-and-validation proposal). This task must not claim its template prose enforces anything.

VERIFIED GROUND (do not re-derive)
- server.go instruction manifest: ORCHESTRATION and TOKEN ECONOMY paragraphs (~line 44). The rule pinning TOKEN ECONOMY is MCP-006 (internal/mcpserver/.spectackle/spec.md:67, applies go:mcpserver.Server.registerTools; its rationale at spec.md:73 says so). SPX-MCP-006 is a DIFFERENT rule about the research tool - do not touch it, do not cite it.
- Manifest-content test precedent to MIRROR (T-0098 pattern, cited in spec.md): multi-substring assertions on the manifest.
- templates/commands/: exactly 8 tmpl files; workflow.md.tmpl has exactly 8 steps, step 7 Check, step 8 Archive+Commit/PR, no make dev anywhere. commands op=gen regenerates command files from templates.
- prompts.go: promptNext (line 158) picks an actionable item and renders its brief; skips blocked and open-needs items. lifecycleLines at :68.
- check fix=true already drafts one backprop proposal per drifted rule (tools.go:1733) - the code-to-spec direction exists; reference it, never duplicate it.

WHAT TO BUILD
1. server.go: one BACKPROP paragraph, <= 700 bytes, stating: the three durable stores (spec.md rules, journal tombstone notes, knowledge artifacts); every completed or rejected item leaves its learning in one of them; the archive/reject note is searched by future sessions - write substance; after every merge, make dev (the server is the product under change). Add an EARS rule via rule op=add binding go:mcpserver.Server.registerTools, mirroring MCP-006's pattern exactly.
2. workflow.md.tmpl: step 7 gains independent re-verification (a fresh-context verifier re-runs the VERIFY block from the diff alone - the implementer's transcript is never the evidence); step 8 gains the note guidance (what changed, what was measured, what was deliberately not done, where the learning landed); new step 9: make dev after merge, one sentence why; new step 10: research capture - every R-item ends consumed or explicitly closed, citing that the server enforces this at the archive gate. TOTAL template growth <= 40 lines, MEASURED: a test counts the template's lines with a hardcoded ceiling and a comment naming the measured pre-change base (the validation round found the prior draft asserted this cap but never measured it).
3. promptNext: the rendered output's FIRST line is the one computed next action per state: draft ungrilled -> grill; draft grilled-with-open-gaps -> close gaps, re-grill; submitted -> approve or reject; approved -> work op=start; active -> implement, then work op=submit; done -> check, then move to=archived with note; blocked -> decide op=answer on the linked ADR. Table test over fixture items in each state asserting the exact first line.
4. docs/agent-workflow.md: BACKWARD PATH section, <= 30 lines (the prior draft left this cap unstated), human-facing mirror of 1-3, plus the independent-verification sentence in the orchestrator role.

NON-NEGOTIABLE PROPERTIES, each with a test
- Manifest SUBSTANCE, computed (mirror T-0098): assertions that the manifest contains BACKPROP, all three store names, the phrase make dev, and the training-signal sentence fragment - multi-substring, not presence-of-paragraph.
- Manifest size ceiling: current measured size + 800 bytes, hardcoded with the base named in a comment.
- Template line ceiling test as in (2).
- promptNext table test as in (3).
- commands op=gen on a temp workspace regenerates files containing steps 9 and 10.
- No lifecycle behavior changes: existing suite passes untouched.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  spectackle call -root <worktree-root> commands '{"op":"gen"}' succeeds; paste the regenerated steps 9-10.
CROSS-VERIFICATION (orchestrator, after done): independent verifier regenerates the commands file, diffs against the report's claim, and re-runs the manifest substance + size tests from the diff alone; verdict recorded in the archive note.

SCOPE: the four named files, generated command files, the one new EARS rule, tests. Do not touch tools.go, lifecycle.go, grill.go. No new tools, no config keys.
ROLLBACK: revert the commit; run commands op=gen once to restore prior command files. The added rule retires via rule op=retire. No stored state.
REPORT BACK: manifest base and final sizes, template base and final line counts, the regenerated steps verbatim, each test's result, anything deliberately not done.

## P-01KYD8HSZ0ERTBFBBEVQD68M4R the commit log is the decision log: every state-machine edge commits with a structured message, and no merge may flatten the trail
kind: proposal
state: submitted
created: 2026-07-25
refs: R-0007, P-01KYD7QT8YE6PAT515BGPQ5VM4
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/wt/wt.go, internal/workspace/workspace.go, CONTRIBUTING.md, docs/agent-workflow.md

REQUIREMENT. Every phase and step taken through the MCP must be followable by a human in git alone: each state-machine edge produces its own commit whose structured message describes the decision (what moved, from where to where, by whom, and the reasoning note), and no merge policy may flatten that trail - pull requests are merged with merge commits, never squashed, because a squash collapses N decisions into one blob and destroys exactly what the per-edge commits create.

WHY THIS EXTENDS RATHER THAN DUPLICATES THE JOURNAL. The journal is the machine's replay log; git is where humans and reviewers already look. Today the two are reconciled only when an orchestrator remembers to commit, in batches with hand-written messages - this session's own history shows multi-edge batch commits whose messages summarize rather than enumerate. Deriving the commit from the journal event at write time makes the two logs agree by construction, with zero agent effort - the driving LLM issues no git command and needs no git knowledge (prior user authorization, extended here from phase checkpoints to every edge).

VERIFIED GROUND. One choke point covers the whole tool surface: gate[T] (tools.go:175-185) wraps every handler under s.mu with preCall/postCall; only the rule tool inlines the identical pattern (tools.go:200-210). Every state edge already appends a journal event with agent identity (journal.Append stamps ag). wt.CommitCode (wt.go:111) is the committing precedent. P-0009 (archived) established one move call per forward jump - the commit granularity inherits it: one call, one edge traversal, one commit, even when the jump skips states.

DESIGN DECISIONS, alternatives rejected:
1. Commit composed FROM the journal event(s) the call appended, in the same gate, after the handler succeeds. Rejected: a background committer (races the next call, decouples decision from commit); a git hook (cannot know the decision, only the diff).
2. Full record IDs in commit subjects and trailers, never display-short forms - a short prefix is the shortest unambiguous prefix AT THAT INSTANT (T-0136) and a commit message is immutable, so a later mint could make an archived subject ambiguous. Machine-readable trailers (Spectackle-Ev, Spectackle-Item, Spectackle-From/To, Spectackle-Agent, Spectackle-Eid) make the log queryable with plain git log --grep and interpret-trailers.
3. The server commits ONLY paths under .spectackle trees it wrote in that call, via explicit-pathspec commit semantics so a user's concurrently staged work is never swept in. Code commits remain the implementer's and the submit path's business.
4. Never push. Remotes stay with the orchestrator and CI.
5. Config git_commits: edges|off, DEFAULT edges - supersedes the phases|off knob drafted in the validation-phase task, whose git section this proposal's engine task subsumes.
6. Merge policy is enforced where it lives: the GitHub repository setting (allow merge commits, disallow squash) is the hard control and is the user's one manual step, documented; CONTRIBUTING.md, the agent workflow docs and the machine-facing instructions all switch from squash to merge so no agent re-introduces flattening.

CROSS-CLONE CAUTION, stated because it is the known sharp edge: two server processes on one checkout can race the git index. The commit step must take the same cross-process serialization the whole-file-rewrite task establishes through coord.db, and must retry on index.lock. The lost-update work (B-01KYD57F line) is the prerequisite ordering.

EXIT CRITERION. Drive one full item lifecycle headlessly (draft, grill, verdict, approve, start, submit, validate, archive); git log --oneline then shows one commit per edge in order, each message carrying the full item ID and the decision note; git log --grep on the item ID returns the complete decision history and nothing else. A squash of that branch is impossible through the documented merge path.

TOKEN COST. Zero marginal agent tokens (commits are side effects); the git overhead is one commit per tool call that wrote state - milliseconds, no network.

ROLLBACK. git_commits: off disarms the engine without a rebuild; reverting the commits removes it. Edge commits already made are inert history. The merge-policy docs revert with the commit; the repo setting reverts by hand.

CHILD TASKS: (1) the edge-commit engine in gate; (2) the merge-policy switch across CONTRIBUTING, docs and instructions. The validation-phase task sheds its narrower git section to the engine at its next redraft.

## T-01KYD8M955E9NBPH27D21E4J9J merge policy: never squash - merge commits everywhere the workflow speaks, so the per-edge decision trail survives into main
kind: task
state: active
created: 2026-07-25
parent: P-01KYD8HSZ0ERTBFBBEVQD68M4R
refs: R-0007
grilled: 2026-07-25
targets: CONTRIBUTING.md, docs/agent-workflow.md, docs/release.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This is a documentation-and-policy task; it changes no Go code.

WHY. The edge-commit engine (sibling task) makes every state-machine edge a commit whose message is the decision. A squash merge collapses N such commits into one, destroying on main exactly the trail the engine creates on the branch - the two policies cannot coexist. The requirement is explicit: pull requests are never squashed; every decision stays visible in the commit log of main.

VERIFIED GROUND (do not re-derive)
- CONTRIBUTING.md's section 'Every change lands through a pull request that auto-merges on green CI' currently prescribes auto-merge (squash) verbatim and documents the two repository settings auto-merge depends on. This is the primary edit site.
- docs/agent-workflow.md describes the orchestrator's git duties; docs/release.md documents the changelog derived from commit messages (commits prefixed docs:/spec:/chore: are excluded) - the edge-commit subject format spectackle(<ev>): ... interacts with that filter and the interaction must be stated, not discovered at release time.
- The workflow template (workflow.md.tmpl) step 8 says Commit/PR per this repo's normal git conventions - it inherits whatever CONTRIBUTING says, and the backward-path task (title: the backward path in every machine-facing surface) owns editing that template. Do NOT edit the template here; the convention text it points to is what you change.

WHAT TO BUILD
1. CONTRIBUTING.md: the auto-merge section switches from squash to MERGE COMMITS, with the one-paragraph rationale (per-edge decisions must survive into main; a squash is a lossy compression of the decision log). Document the THREE repository settings now required, each with why: Allow auto-merge (unchanged), a required status check on main (unchanged), and Allow merge commits ON / Allow squash merging OFF - the last is the hard enforcement and is a one-time manual step for the repository owner; name it as such, the repo cannot enforce it from inside.
2. Same section: state the branch hygiene consequence - merge commits preserve every branch commit, so branches must carry clean per-edge and per-change commits rather than fixup noise; wip commits are amended or rebased BEFORE the PR opens, never squashed at merge time.
3. docs/agent-workflow.md: the orchestrator role's git duties gain two sentences: merge method is merge commit, never squash; the edge commits are server-made and the orchestrator neither replicates nor batches them.
4. docs/release.md: one paragraph on changelog interaction - state whether spectackle(<ev>) subjects appear in release notes and the chosen filter (recommend: exclude spectackle(*) subjects from the changelog exactly like docs:/spec:/chore:, since they narrate process, not shipped change; implement by documenting the goreleaser filter addition needed and flag that .goreleaser.yaml itself is OUT of this task's file scope - name the follow-up explicitly in the report if you cannot make the filter change without touching it).
5. Every edited sentence must remain true if the edge-commit engine is disabled (git_commits: off) - the merge policy stands on its own; do not couple the two beyond the rationale sentence.

NON-NEGOTIABLE PROPERTIES
- No remaining instruction to squash anywhere in the repository's markdown: a grep for squash across *.md shows only the new policy text explaining why squash is forbidden (test with a script or a Go doc test - state which).
- The three settings are documented with their failure modes (what silently breaks when each is missing), mirroring the existing section's style, which already does this for the first two.
- CONTRIBUTING.md still renders as coherent prose - the section reads as one policy, not a patch seam.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race (docs tasks still run the suite - nothing may break) ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  grep -rn squash --include='*.md' . - paste the output; every hit must be policy text forbidding it.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the grep and reads the CONTRIBUTING section cold, then answers from the text alone: what merge method, which three settings, who flips the third - if the text does not answer all three, the task is not done. Verdict recorded in the archive note.

SCOPE: the three named markdown files. No Go code, no templates (backward-path task owns them), no .goreleaser.yaml, no workflow yml.
ROLLBACK: revert the commit; the repository settings revert by hand.
REPORT BACK: the final CONTRIBUTING section verbatim, the grep output, the release-notes filter decision and any named follow-up, anything deliberately not done.

## P-01KYD9466KEPWBV2RBK7EQM202 review and validation are recorded independent verdicts: grill reviews the draft with feedback and a research path, a validation phase judges the implementation, both bound to reviewer identity
kind: proposal
state: submitted
created: 2026-07-25
refs: R-0007, P-01KYD7QT8YE6PAT515BGPQ5VM4, R-0005
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/journal/journal.go, internal/lifecycle/lifecycle.go, docs/lifecycle.md

Supersedes the draft in refs: its central claim - identity binding as a computed invariant - was proven overclaimed by this set's own validation round against the real code, and the corrected design plus the honest limit are below. Everything else re-recorded intact.

PROBLEM. Two review moments exist in the loop and neither is a review. Before implementation, grill renders a pack and stamps a date - the stamp records that rendering happened, not that anyone read the pack or fed anything back; twelve grills in this repository's history changed zero bodies. After implementation there is no phase at all: the submit gate runs commands, check scans the workspace, done rolls into archived on the orchestrator's say-so - nobody is charged with judging correctness, test honesty, benchmark honesty, or completeness, and whatever the orchestrator notices reaches the implementer as chat, not as a recorded finding the next round is held to.

DESIGN DECISIONS, with rejected alternatives:
1. NO NEW LIFECYCLE STATES. done -> active is the single sanctioned backward hop (docs/lifecycle.md:142) with a reopen counter, feedback.max_rounds and escalation (SPX-SWM-007). Validation gates done -> archived and reopens through the existing hop. Rejected: a new validating state - touches every state-order comparison and replay path for a distinction the reopen counter already expresses.
2. VERDICTS ARE RECORDED EVENTS BOUND TO CONTENT AND IDENTITY. A verdict is a journal event carrying the reviewing agent's identity and the hash (or git SHA range) of what was reviewed; the server refuses a verdict whose identity matches the item's author or implementer, and refuses a verdict from an EPHEMERAL identity - server.go:173 reads SPECTACKLE_AGENT once per process and falls back to coord.GenName(), a random name, so without the ephemeral refusal a per-call client with the env unset would pass the author-check by pure chance (found by this set's validation). A shared resident connection carries one identity for all callers (B-0002 lineage), so verdicts are per-call stdio operations with a deliberately set agent name - the docs say exactly that.
THE HONEST LIMIT, stated here because the earlier draft claimed too much: what the server computes is deliberate-identity divergence, not independence. It defends against FORGETTING to use a separate reviewer - the failure mode R-0007 documented as fakeable-by-forgetting - not against a driver minting a second name purely to clear the gate while sharing every blind spot. That residual is accepted, stated in code comments and docs, and mitigated only by process guidance (fresh context or different model for reviewers), which the server cannot verify. Rejected alternative: claiming the check IS independence - that is how overclaims calcify.
3. THE SERVER RENDERS AND RECORDS; AGENTS JUDGE. The server computes the packs and refusals, records verdicts, gates moves. The judgment - reading, deciding, writing findings - is agent work in a fresh subagent. Findings are floored (non-empty on fail, warn under 80 chars) and capped (2000 bytes stored): the one artifact whose value depends on the reviewer thinking must be neither empty nor unbounded. Rejected: the server scoring quality itself - that is how the word-presence checks happened.
4. GRILL MAY DEMAND RESEARCH, NOT PERFORM IT. When the pack's computed classes surface unknown territory (uncovered target paths, zero history/rejection hits), grill emits a research-needed record counting as an open gap until the item cites an R-item that names the flagged path or term VERBATIM (token match, not semantic scoring - and not any-R-item, which this set's validation showed was gameable). Grounding: R-0005 is the defect class - thirty of thirty-two language recognizers shipped with gaps because novel territory was entered without a study; a demanded R-item is the study. Rejected: grill spawning research - the server cannot run agents and a blocking tool violates SPX-MCP-001.

WHEN EACH STEP HAPPENS: research (on grill demand) and grill-with-verdict between draft and approved - move to=approved gates on a clean independent review verdict. Implementation active -> done as today. Validation between done and archived - move to=archived gates on a clean independent validation verdict; findings reopen done -> active with the findings as the implementer's next brief, counting a round; max_rounds escalates to blocked as today. Nothing else moves.

CHILD TASKS: one supersedes the earlier grill-verdict draft (computed classes, verdict event, identity+ephemeral refusals, research-demand); one builds the validation phase (pack, verdict, gate, reopen feedback, note auto-fill from the verdict). Git checkpoint commits are OWNED by the commit-log-is-the-decision-log proposal, not here.

TOKEN BOUNDS. Verdict events are one journal line; findings capped at 2000 bytes; packs budget-truncated; independence checks O(item's journal events), already loaded. Mutant-kill and oracle-ratchet stay out of scope until the anti-ceremony lens re-runs against measured costs.

EXIT CRITERION. On this repository: a draft receives an independent review verdict from a second, deliberately named agent identity and cannot reach approved before; a done item with a planted vacuous test receives a validation finding, reopens with the finding as its brief, and cannot reach archived until re-validation is clean; an ephemeral-identity verdict is refused with the exact record.

ROLLBACK. Both gates sit behind config strictness mirroring feedback.grill (require|warn); removing the key returns to warn, reverting the commits returns to today. Verdict events in journals are inert history for a reverted server.

## T-01KYD94KP4FBHR0RGR2P8CZNBZ grill computes its critique and stamps a verdict; the verdict is an independent review event with feedback and a research-demand path; the fakeable word-checks are deleted
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD9466KEPWBV2RBK7EQM202
refs: R-0007, T-01KYD87YYZFSJVGX74JG2HD4V3
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/journal/journal.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This task supersedes the rejected draft it cites in refs; everything that draft specified remains in force here and is restated - nothing is inherited by pointer.

WHY. R-0007's organizing finding: every review mechanism splits into a server-computed half the author cannot fake and a written half they can, and grill today is almost entirely the written half. Twelve grill events exist in this repository; every one fired 0-91s after the item's create event and not one body was ever revised in response. A date stamp records that a pack rendered, not that anyone reviewed anything. This task makes grill compute what it can compute, and makes the review itself a RECORDED, INDEPENDENT verdict: a journal event carrying the reviewing agent's identity and the hash of the body it reviewed, refused when the reviewer is the item's author, gating approval. The reviewing agent is a fresh subagent by design - the server enforces the independence it cannot create.

VERIFIED GROUND (do not re-derive)
- grill.go:97 renders #questions via grillQuestions(it); :101 stamps it.Grilled unconditionally; :165 applies briefHeuristics to child-task bodies.
- briefHeuristics (grill.go:177): len<300, no "/", no "go test"/"make". grillQuestions (grill.go:242): substring tests for scope/rollback/exit-criterion; plus hasRecordedDeliberation (structural, checks refs and rejected alternatives - KEEP).
- Move gate: tools.go:1329-1338 - feedback.grill=require refuses ungrilled proposals, else warns. Item.Grilled is a plain string, kept on reopen (lifecycle.go:380), replayed via Gr.
- Journal: every event stamps ag (journal.Append fills e.Ag from root.Agent); event kinds live in journal.go:31-46; EvGrill exists. The item's author identity is the create event's ag; a worktree implementer's identity is the start/submit events' ag.
- grillIn.Budget defaults to 1500 with truncation + resume cursor.
- SPECTACKLE_AGENT sets the per-process agent identity; per-call stdio clients each carry their own.

WHAT TO BUILD
1. COMPUTED CLASSES in the pack, each one output line per finding, all counted into open=<n>:
   a. path-existence: path-shaped tokens in the body that do not exist in the worktree -> "g nopath <token>".
   b. verify-executability: VERIFY-block lines matching a known-bad table -> "g badverify <pattern>". Seed with exactly two recorded failures: lint -root (B-0010) and reading $? after a pipe. The table is a var in grill.go.
   c. irreversibility-from-targets: targets matching journal.ndjson, coord.db, SchemaStamp/migration paths, or target count >= 8 -> "g irreversible <target>" / "g blast <n> targets", demanding a RESTORE or ROLLBACK section heading exist. STATED LIMIT, verbatim in a code comment: this is a TRIPWIRE against omission, not a verification of substance - T-0137 gamed the old word-check with a well-formed paragraph and a heading requirement is gameable the same way; substance is judged by the independent reviewer's verdict (this task) and declared-vs-landed divergence (T-0135's 4-declared/15-landed) is NOT detectable pre-implementation here - it is the validation phase's #diff offscope/untouched computation (sibling task).
   d. environment differential, five axes, each anchored to a recorded defect: primary-branch-name (B-0004), git-dir-shape file-vs-dir (B-01KYD1G9K), root-kind worktree-vs-checkout (B-0002), process-topology shared-vs-per-call (B-01KYD57F), path-normalization case/sep (T-0136's d-bus finding). Render "e <axis> live=<v> tests=<v|absent>"; tests= from a static scan of target packages' _test.go files. absent counts as open ONLY when the item's targets touch that axis's subsystem; state the per-axis scoping condition in the report.
   e. research-demand: when the item's targets include a path no rule's dir or applies covers AND grillRejections plus the history search return zero hits for the item's title terms, emit "g research-needed <topic>" - the item is in unknown territory and the pack cannot substitute for a study. The gap closes when the item's refs cite an R-item (any state). Novelty detection is those two computed signals only - no semantic scoring.
2. VERDICT EVENT. New journal kind EvReview. A new grill op records the verdict: grill op=verdict id=<item> pass=<bool> findings=<text>. The server computes bodyHash = sha256 of the item's body at verdict time and writes it into the event. REFUSALS, each with a dense error: (i) verdict agent ag equals the create event's ag -> "! REVIEW E <id> reviewer is the author - use a fresh agent identity"; (ii) open>0 from the latest pack render and pass=true -> refused, the computed gaps are not the reviewer's to waive; (iii) no pack was rendered for the current body (stored render-hash mismatch) -> refused, the reviewer must grill the current body first; (iv) pass=false with empty findings -> refused, a failing verdict must say why - the findings become the author's next brief; (v) the verdict caller's identity is EPHEMERAL -> refused: server.go:173 reads SPECTACKLE_AGENT once per process and falls back to coord.GenName(), a RANDOM name - the anti-ceremony validation proved a per-call stdio client with the env unset gets a fresh random identity every call, so the author-check would pass by pure chance with zero independence. The server therefore records whether each event's identity was env-set or generated (one boolean on the event, or a name-shape check if GenName names are reliably distinguishable - decide from the code, justify), and verdicts from generated identities are refused: "! REVIEW E <id> anonymous reviewer - set SPECTACKLE_AGENT to a deliberate name". The RESIDENT-SERVER corollary, stated in docs and the refusal hint: a shared resident connection carries ONE identity for all its callers (the B-0002 lineage), so review and validation verdicts are per-call stdio operations with an explicitly set agent name - the docs instruct exactly that invocation. FINDINGS FLOOR, layered honestly: on pass=false, findings must be non-empty (hard refusal above) and under 80 characters draws a one-line warn record (tripwire against token-thin reviews, gameable by padding and known to be - same layering as the research-gate note floor); on pass=true findings stay optional, a clean pass needs no essay. findings text is journaled verbatim and rendered by get on the item so the author's next revision sees it.
IDENTITY LIMIT, verbatim in a code comment and in docs (the independence validation of this set demanded this callout): what the refusal computes is ag-string divergence, not independence. It defends against FORGETTING to use a separate reviewer - the failure mode R-0007 documented - not against a driver minting a second identity purely to clear the gate while sharing every blind spot. That residual is accepted and stated, never claimed away; process guidance (different model or fresh context for reviewers) lives in the workflow docs, outside what the server can verify.
3. STAMP AND GATE. Grilled becomes "<date> open=<n>" written at pack render (n = computed findings). move to=approved under feedback.grill=require additionally requires a passing EvReview whose bodyHash matches the CURRENT body and whose ag differs from the author - a body edited after review needs re-review by construction. Without require: warn lines, same computations. Legacy bare-date stamps: treated as no verdict (require refuses, warn names why).
4. DELETE: the scope/rollback/exit-criterion substring questions and the short-body/no-path/no-verify heuristics. ALSO DELETE the substring half of hasRecordedDeliberation: the anti-ceremony validation of this task set found its second path is strings.Contains(body, "rejected") (grill.go:275) - a bare word-presence test of exactly the species this task removes, satisfiable by writing "we rejected X" without weighing anything. The deliberation check becomes refs-only: an ADR, research, or rejection-tombstone ref counts; prose never does. KEEP: the refs path, grillTests, grillRejections. #questions shrinks to the refs-only deliberation check.
5. BUDGET. New sections respect grillIn.Budget; computed findings and the verdict line render before lower-value sections; the verdict line is exempt from truncation.

NON-NEGOTIABLE PROPERTIES, each with a test
- Author-verdict refusal: create as agent A, verdict as A refused with the exact record; as agent B accepted. Use two Server instances with distinct SPECTACKLE_AGENT (the twoAgents helper pattern in swarm_test.go).
- Hash binding: verdict as B, then edit the body (re-draft path or direct item write in the test), then move to=approved under require -> refused naming stale review; re-grill + re-verdict -> approved succeeds.
- Waiver refusal: plant a nopath finding, verdict pass=true refused while open>0.
- research-demand: a fixture item targeting an uncovered path with zero history hits trips it; adding an R-item ref closes it.
- Each computed class fires on a synthetic item constructed to trip exactly it and stays silent otherwise.
- Word padding changes nothing: a body containing scope, rollback, exit criterion still counts its computed gaps.
- Byte bound, COMPUTED not self-reported (the independence validation of this task set found the prior draft left this as prose a same-model verifier would rubber-stamp): a Go test renders the pack on a fixed synthetic item and asserts the output stays under a hardcoded byte ceiling, the ceiling committed with a comment naming the measured pre-change base (mirror the manifest-size test pattern). The P-0088 before/after counts still go in the report, but the regression bound is the test.
- Red-run: the move-gate test and the author-refusal test are written first and shown failing against current code; paste both failing outputs.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional - the flag form is entry one of your own known-bad table)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
CROSS-VERIFICATION (orchestrator, after done): an independent verifier with a DIFFERENT agent identity re-runs the refusal and hash-binding tests from the diff alone and additionally performs one real review: grill a live draft in the worktree, write a verdict, confirm the gate honors it. Verdict recorded in the archive note.

SCOPE: grill.go, the move gate in tools.go, the EvReview constant in journal.go, tests. Do not touch the item model (Grilled stays a string), lifecycle.go's state machine, templates, prompts, or the validation phase (sibling task). tools.go is shared with FOUR other open tasks (validation phase, coverage, research gate, edge-commit engine) plus the rule-edit draft - the lease serializes all of them; do not run concurrently with any.
ROLLBACK: revert the commit. Stamps "<date> open=<n>" stay parseable via the legacy bare-date path you keep; EvReview events in journals are inert history for a reverted server - verify a pre-revert journal still replays post-revert and state it in the report.
REPORT BACK: where each class and refusal is computed, the per-axis scoping conditions, both byte counts, each test's real result including both red-runs, the replay-after-revert check, anything deliberately not done.

## T-01KYD94M3EFXCBVRVWZCS5KBE9 validate: the post-implementation phase - computed pack over the diff, independent verdict gating archive, findings reopen the item as the implementer's next brief
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD9466KEPWBV2RBK7EQM202
refs: R-0007, T-01KYD87ZA6F83AKH7THFKBBFZA
grilled: 2026-07-25
targets: internal/mcpserver/validate.go, internal/mcpserver/tools.go, internal/journal/journal.go, internal/workspace/workspace.go, internal/wt/wt.go, docs/lifecycle.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: the grill-verdict task (grill computes its critique and stamps a verdict) must be MERGED first - this task mirrors its verdict/identity/hash machinery and shares the move path in tools.go. Do not start while it is open.

WHY. After implementation the loop has no judge. The submit gate runs commands (pass/fail, no judgment), check scans workspace consistency, and done rolls into archived on the orchestrator's say-so. Nobody is charged with: is the implementation correct against its brief, do the new tests actually test the change, are benchmark claims honest, is the work complete. And when the orchestrator does notice something, the feedback reaches the implementer as chat - unrecorded, unsearchable, not binding on the next round. This task builds the validation phase: a computed pack over the item's real diff, an independent recorded verdict gating done -> archived, and findings that reopen the item through the EXISTING done -> active hop (docs/lifecycle.md:142) so the feedback IS the next brief and rounds count toward the existing feedback.max_rounds escalation (SPX-SWM-007). No new lifecycle states.

VERIFIED GROUND (do not re-derive)
- done -> active is the sanctioned backward hop with a reopen counter and escalate-to-blocked at max_rounds; lifecycle keeps Grilled on reopen (lifecycle.go:380). Rounds already replay.
- Journal events stamp ag; the implementer's identity is the item's start/submit events' ag; EvGrill is the pattern for a feedback event kind. The grill-verdict task (NEEDS) lands EvReview and the identity/hash refusal pattern - MIRROR it, do not reinvent it.
- The item's diff is recoverable: the submit path merges a worktree branch; the merge commit and the branch (spectackle/<item-id>) name the change. git diff against the pre-merge parent bounds the reviewed surface. For items implemented without a worktree, fall back to the diff of the commits whose messages cite the item ID; when neither exists, the pack says so and the verdict proceeds on pack-absent evidence - validation must not be skippable just because attribution is hard, but it must say what it could not see.
- workspace config feedback block exists (FeedbackCfg, workspace.go ~:53) with Grill string knob - add Validate string knob beside it, same semantics (require|warn, default warn).

WHAT TO BUILD
1. A validate TOOL (new file internal/mcpserver/validate.go), read-computed like grill, budget-truncated (default 1500), sections all computed:
   #diff - files changed with +/- counts, SPLIT into: declared targets touched, declared targets NEVER touched (finding "v untouched <target>"), files changed OUTSIDE targets (finding "v offscope <file>"). Bounded 20 lines + tail. This is where declared-vs-landed divergence is caught - T-0135 declared four files and landed fifteen, and no pre-implementation check can see that; this computation is the mechanism that would have.
   #tests - test honesty, computed, each a finding line: (a) production symbols added/changed in the diff with zero references from any test in the diff or existing tests -> "v untested <symbol>" (graph + diff parse, cap 10); (b) anti-vacuity over CHANGED test files only: a subtest loop body containing no assertion call, a range-over-collection whose assertions sit only inside the range with no emptiness guard -> "v vacuous <file:line>" (AST, cap 10); (c) a test file changed with zero production files changed and the item is kind=bug -> "v testonly - bug fix with no production change" (the fix-in-test smell).
   #bench - only when the diff touches Benchmark funcs or *_bench_test.go: (a) a Benchmark whose loop does not consume b.N or b.Loop -> "v fakebench <func>" (AST); (b) benchmark numbers claimed in the item's report/notes with no matching Benchmark func in the diff -> "v benchclaim <name>". Both classes capped at 10 + tail like #tests (the validation round flagged the missing per-section cap). The validator agent re-runs ONLY the named benchmarks with -benchtime=1x as an execution proof; performance regressions are its judgment, not a server computation.
   #verify - the declared gate/verify commands and their last recorded result from the submit gate journal trail, so the validator sees what was proven versus asserted.
2. DIFF BINDING BY SHA: the validation pack and verdict bind to the git commit range (merge-base of the item's branch to its merge commit; fallback below), recorded as SHAs in the EvValidate event - a SHA range is content-addressed by git itself, so the stale-verdict check is a SHA comparison, no bespoke hashing. CITATION RULE for the no-worktree fallback, exact (the independence validation flagged the prior vagueness): a commit cites the item iff its message carries the FULL item ID as a word-delimited token in the subject (the submit path's existing "spectackle <item-id>: ..." convention) or a Spectackle-Item trailer equal to the full ID (the edge-commit engine's format, sibling proposal). Short prefixes never match - a prefix is unambiguous only at the instant it was rendered. When neither worktree nor citing commits exist, the pack states pack-absent evidence and the verdict proceeds on it - validation must not be skippable because attribution is hard, but it must say what it could not see.
COMMIT CHECKPOINTS ARE NOT THIS TASK: the per-edge commit of .spectackle writes (including verdict events) is owned by the edge-commit engine task under the commit-log-is-the-decision-log proposal (title: edge-commit engine in gate). Do not implement any git committing here; this task only READS git for the diff and the SHA binding.
3. VERDICT: validate op=verdict id=<item> pass=<bool> findings=<text>. Journal kind EvValidate. FINDINGS RULES mirror EvReview exactly: pass=false with empty findings refused (the findings are the reopened item's next brief); under 80 chars draws a warn tripwire; stored findings are capped at 2000 bytes with a truncation marker (the validation round found the prior draft left this field unbounded - an LLM-written field replayed on every future get must have a ceiling). Ephemeral-identity refusal mirrors EvReview: verdicts from a generated (env-unset) agent identity are refused; review and validation are per-call stdio operations with a deliberate SPECTACKLE_AGENT. NOTE AUTO-FILL, closing the two-rounds-open empty-note defect: when move to=archived on a task or bug carries no note, the server writes the note FROM the passing EvValidate verdict (pass, findings summary, validator identity, SHA range) - the archive note stops being fakeable prose because it is derived from the recorded verdict, with zero agent effort. An explicit note, when given, is appended after the derived part, never instead of it. Other refusals mirror EvReview: same-agent (verdict ag equals any start/submit ag of the item, OR the create ag when no start exists) -> "! VALIDATE E <id> validator implemented this - use a fresh agent identity"; pass=true while the pack's computed findings > 0 -> refused (computed findings are not waivable; the validator judges ON TOP of them, never instead of them); diffHash mismatch (diff changed since last pack render) -> refused, re-render first.
3. GATE + FEEDBACK LOOP: move to=archived (and the shortcut that implies it) for kind=task and kind=bug requires, under feedback.validate=require, a passing EvValidate with matching diffHash and independent ag; warn mode warns. A verdict with pass=false REOPENS the item: server performs done -> active, increments the existing round counter, and writes the findings into the journal; get on the reopened item renders the findings as the FIRST section - the feedback is the brief. max_rounds exhaustion escalates to blocked exactly as today - no new escalation path.
4. Documentation: docs/lifecycle.md gains the validation hop in its state diagram prose (done -> archived gated; done -> active on findings), one short section. The workflow template is OWNED by the backward-path task under the backpropagation proposal - do not edit it here; note the dependency in your report instead.

NON-NEGOTIABLE PROPERTIES, each with a test
- Implementer-verdict refusal: start+submit as agent A (worktree e2e path exists in worktree_e2e_test.go to crib), verdict as A refused; as B accepted.
- Waiver refusal: plant an untouched target, pass=true refused while findings>0.
- Reopen loop: verdict pass=false moves done -> active, rounds increments, get renders the findings first, and a second implementation round followed by clean re-validation archives.
- Diff binding: verdict, then one more commit citing the item, then move to=archived under require -> refused stale; re-render + re-verdict -> succeeds.
- Each computed finding class fires on a fixture built to trip exactly it (vacuous subtest, fake bench without b.N, untouched target, offscope file) and stays silent on clean fixtures.
- Escalation unchanged: exhausting max_rounds through repeated failing verdicts lands in blocked with the ADR-item exactly as SPX-SWM-007 specifies (existing tests untouched).
- Cost: one validate call on a real merged item in this repository - report wall time and output bytes; must satisfy SPX-MCP-001 (2s warm, 1 MiB reads).
- Red-run: the archive-gate test written first, shown failing against current code; paste the failing output.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
CROSS-VERIFICATION (orchestrator, after done): an independent verifier with a different agent identity performs one real validation on a merged item in the worktree - renders the pack, records a verdict, confirms the gate honors it and a false verdict reopens - from the diff alone. Verdict recorded in the archive note.

SCOPE: validate.go (new), the move-gate addition in tools.go, EvValidate in journal.go, the FeedbackCfg knob in workspace.go, docs/lifecycle.md, tests. Do not touch grill.go (the NEEDS task owns it), the state-order table, templates, or prompts. tools.go is shared with sibling tasks - the lease serializes.
ROLLBACK: revert the commit; feedback.validate absent means warn, so removing the key alone already disarms the gate. EvValidate events are inert history for a reverted server - verify replay of a pre-revert journal and state it in the report.
REPORT BACK: where the diff is recovered from and the fallback used, each refusal's implementation, the reopen wiring into the existing rounds machinery, wall time and bytes on the real-item run, each test's real result including the red-run, anything deliberately not done.

## T-01KYD94MG8FBMTJP5CPC62PCYM edge-commit engine in gate: every tool call that writes .spectackle state commits it with a structured decision message composed from its journal events
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD8HSZ0ERTBFBBEVQD68M4R
refs: R-0007, T-01KYD8M8RXEXPVTCWTMY962PQQ
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/mcpserver/server.go, internal/wt/wt.go, internal/workspace/workspace.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: the coord.db serialization task (title: serialize server-side whole-file rewrites through the coord.db lock table) must be MERGED first - two server processes on one checkout race the git index exactly like they race work.md, and the commit step belongs inside the same cross-process serialization. Do not start while it is open.

WHY. The requirement is followability: a human reading git log alone must see every decision - each state-machine edge as its own commit, message carrying what moved, from where to where, by whom, and the reasoning note. Today the journal knows this and git does not: reconciliation happens only when an orchestrator remembers to commit, in batches whose messages summarize. Deriving the commit from the journal event at write time makes the two logs agree by construction, with zero agent effort - the driving LLM issues no git command (user authorization: fully automatic, without the LLM doing anything).

VERIFIED GROUND (do not re-derive)
- gate[T] (tools.go:175-185) wraps every tool handler: s.mu.Lock, preCall, handler, postCall. The rule tool inlines the identical pattern at tools.go:200-210. These two sites are the complete write surface - no .spectackle write happens outside a tool call.
- Every state edge appends a journal event with agent identity (journal.Append stamps ag, eid, timestamp). Event kinds: journal.go:31-46.
- wt.CommitCode (wt.go:111) commits in a worktree today - the plumbing precedent; reuse or extract, do not duplicate git exec logic.
- P-0009 (archived): one move call per forward jump. Commit granularity inherits it: ONE call = ONE edge traversal = ONE commit, even when the jump skips states (draft->active is one decision, one commit).
- workspace config: FeedbackCfg pattern at workspace.go:53 for adding the knob.

WHAT TO BUILD
1. CAPTURE: during a tool call, record which journal events the call appended and which .spectackle paths it wrote (choose the mechanism from reading the code - an events buffer on the Server filled by the append path, or a pre/post diff of journal lengths plus a written-paths set; justify the choice in the report; it must be exact, not a glob of everything dirty).
2. COMMIT STEP in gate (and the rule tool's inline twin), after the handler returns success, before postCall renders: if the call wrote .spectackle state and git_commits=edges, commit EXACTLY the .spectackle paths this call wrote, via explicit-pathspec commit semantics (git commit restricted to named paths so a user's concurrently staged work is NEVER swept in - state which git invocation form guarantees this and prove it with the staged-bystander test below). One commit per call. A call that wrote nothing commits nothing. A failed handler commits nothing.
3. MESSAGE FORMAT, structured and immutable-safe:
   subject: spectackle(<ev>): <full-item-or-rule-id> <from>-><to | one-clause decision>
   body: the decision note verbatim (move notes, verdict findings, rejection reasons; empty note renders the item title).
   trailers: Spectackle-Ev, Spectackle-Item (FULL ID - a display-short prefix is unambiguous only at that instant, T-0136, and commits are immutable), Spectackle-From, Spectackle-To (when applicable), Spectackle-Agent, Spectackle-Eid.
   Multi-event calls (a move that cascades child updates) still produce one commit: primary event in the subject, sibling events as additional Spectackle-Eid trailers.
4. CONFIG: git_commits: edges|off in workspace config, DEFAULT edges. off produces byte-identical tool behavior and zero commits (test). This knob supersedes the phases|off draft in the validation-phase task; that task sheds its git section at its next redraft - do not implement anything from it here beyond what this body states.
5. SAFETY RAILS, each tested: no git repo or .spectackle gitignored -> skip silently, tool call succeeds (a checkpoint is a bonus, never a failure mode); index.lock contention -> bounded retry with backoff, then skip with one journal-only warning event, never a tool error; never push, never amend, never commit paths outside .spectackle trees; detached HEAD or mid-rebase -> skip silently.
6. REPLAY RECONCILIATION - the B-0006 question, answered (the anti-ceremony validation raised it as severe and it must be in your ground truth): wt.CommitCode deliberately EXCLUDES .spectackle (":(exclude).spectackle", comment: live replay input, deliberately uncommitted), and MergeMain's preserveSpectackle exists because a worktree and the primary checkout touching the same journal paths concurrently was defect B-0006. The edge engine does NOT change any of that: TREE CONTENT of .spectackle on main remains owned by replay + preserveSpectackle exactly as today - the code merge continues to exclude .spectackle, preserveSpectackle stays authoritative, and journals are never git-merged (append-only files conflict trivially; the replay machinery exists because git merge cannot reconcile them). What the engine adds is HISTORY: edge commits made inside a worktree live on that worktree's branch, and because the merge policy is never-squash (sibling task), the merge commit's second parent keeps them readable in git log forever - the decision trail survives while the tree stays replay-owned. Consequence to state in code comments and prove with a test: after a submit, main's .spectackle content equals what replay produced (byte-check against a no-engine control run), AND git log --grep on the item ID shows the worktree-side edge commits through the merge parent. One validator recommended defaulting this feature off as redundant with the journal's eid/ag event log; the requirement is explicit that the trail must be in git for humans, so the default stays edges and the dissent is recorded here.
7. SUBMIT-PATH COEXISTENCE: work op=start/submit/abort already create branches, merge and commit code (internal/wt). The edge engine must not double-commit what the submit path commits: during a work call the engine commits only the journal/state writes the call made OUTSIDE the code merge (read the submit flow first; state in the report exactly which commits a submit now produces and why each exists).

NON-NEGOTIABLE PROPERTIES, each with a test
- One edge, one commit: draft then move to=approved then move to=rejected produces exactly three commits, subjects matching the format, trailers parseable by git interpret-trailers.
- Forward-skip is one commit: move draft->active yields one commit whose Spectackle-From/To are draft/active.
- Decision visibility: git log --grep=<full-item-id> returns that item's complete edge history and nothing else, on a fixture driving the full lifecycle.
- Staged-bystander: stage an unrelated source file, run a draft call; the edge commit contains only .spectackle paths, the bystander stays staged and uncommitted.
- off knob: byte-identical journal and tool output, zero commits.
- No-git workspace: calls succeed, zero errors.
- Concurrency: the twoAgents topology (two servers, one root) driving concurrent drafts produces one commit per call with no index.lock failure surfacing to either caller (this is the test that requires the NEEDS ordering).
- Failed handler: a refused move (e.g. unknown ID) commits nothing.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  In the worktree: run one draft + one move headlessly, paste git log --format=full -3 output showing the structured messages.
  Red-run: the one-edge-one-commit test written first, shown failing against current code; paste the failing output.
CROSS-VERIFICATION (orchestrator, after done): an independent verifier re-runs the staged-bystander, off-knob and concurrency tests from the diff alone, then drives one real lifecycle and reads the log; verdict recorded in the archive note.

SCOPE: gate and the rule inline twin in tools.go, the capture mechanism, git plumbing shared with internal/wt, the config knob, tests. Do not touch lifecycle.go's state machine, grill.go, templates, prompts, or the merge-policy docs (sibling task).
ROLLBACK: git_commits: off disarms without rebuild; reverting the commit removes the engine; existing edge commits are inert history.
REPORT BACK: the capture mechanism chosen and why, the exact git invocation form for pathspec-only commits, the submit-path commit inventory, each test's real result including the red-run, measured wall-time overhead per call (report the delta on 10 sequential draft calls), anything deliberately not done.

## T-01KYD9JTSDFYHTEM8B6YXX6NSP steps are judgments, automations are implications: the reviewer's verdict is authoritative over computed findings, per-finding, with recorded reasons
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD9466KEPWBV2RBK7EQM202
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/validate.go, internal/mcpserver/tools.go, .spectackle

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: BOTH verdict tasks must be MERGED first - the grill-verdict task (title: grill computes its critique and stamps a verdict) and the validation-phase task (title: validate: the post-implementation phase). This task AMENDS one clause in each after they land; it is deliberately a separate, later task so the amendment is a visible recorded decision rather than a silent redraft.

THE PRINCIPLE, stated by the user as a standing design rule and to be pinned as a contract: state-machine steps are AGENT JUDGMENTS - drafting, reviewing, deciding, implementing, validating. Server automations - computed critique classes, evidence sweeps, edge commits, note auto-fill, next-step hints - are IMPLICATIONS of those steps: they exist to save the agent purely mechanical work (token economy), and they are never themselves a step, never a substitute for one, and never an authority above one. Grill is a review process performed by a reviewing agent; its computed pack is the evidence delivered to that reviewer so the reviewer spends no tokens on mechanical discovery - the pack is not the review.

WHAT THIS AMENDS, precisely. Both verdict tasks carry the refusal: pass=true while computed findings > 0 is refused, computed gaps are not the reviewer's to waive. That clause makes the automation authoritative over the judgment step - exactly the inversion the principle forbids. It was drafted to prevent silent waiving; the correct mechanism preserves that property without the inversion:
1. PER-FINDING ADDRESSAL replaces the blanket refusal. Every computed finding in the pack carries a stable finding key (class + subject, e.g. nopath:docs/missing.md). A verdict must ADDRESS every open finding key, each in exactly one of two ways: fixed (the re-rendered pack no longer emits it) or waived with a per-finding reason recorded in the verdict event (mirror of the unconsumed-ok suppression design in the evidence-sweeps task: explicit, per-item, visible, never blanket). The server refuses a verdict - pass OR fail - that leaves any finding key unaddressed: "! REVIEW E <id> unaddressed findings: <keys>". This is automation in its correct role: it cannot overrule the reviewer, but it mechanically guarantees the reviewer SAW everything, which is the only thing a machine can actually verify.
2. THE GATE KEYS ON THE VERDICT ALONE. move to=approved (and to=archived for validation) checks: passing verdict, identity and hash/SHA binding valid - and nothing else. The open=<n> stamp remains rendered evidence, not a gate input. Waived findings render in the pack as "g waived <key> <reason>" so the next reader sees the judgment, and waivers are hash-bound like the verdict: a body edit clears them with the verdict.
3. WORDING SWEEP: the grill and validate tool descriptions, the workflow docs and the instruction manifest must describe grill/validate as review performed by an agent with server-computed evidence - not as checks that pass or fail. Rendered refusal texts follow: the server never says the pack failed the item; it says which findings await the reviewer's judgment.
4. CONTRACT: one EARS rule at the root bundle pinning the principle, composed via rule op=add, applies-bound to the gate function so the coverage definition counts it: WHEN a lifecycle gate evaluates an item, the server SHALL treat recorded agent verdicts as the sole gating authority and computed findings as evidence requiring addressal, never as an independent veto.

WHY THE ANTI-FAKING PROPERTY SURVIVES (state this in code comments): the blanket refusal prevented silent waiving by force; per-finding waivers prevent it by RECORD - a waiver without a reason is refused, a blanket waiver is impossible (keys are enumerated), and every waiver is permanently attributed to the reviewer identity in the journal. R-0007's fakeability split is respected: what the author can fake is unchanged (nothing new is written by the author); what the reviewer asserts is now exactly as recorded and attributable as the verdict itself.

NON-NEGOTIABLE PROPERTIES, each with a test
- A verdict leaving one finding key unaddressed is refused naming exactly that key; addressing it as waived with a reason passes; with an empty reason, refused.
- A blanket attempt (waive-all without keys) has no API shape that succeeds - prove by construction, state in the report.
- The gate approves on a passing verdict with waivers present, and refuses on a stale verdict after a body edit (waivers cleared with it).
- The re-render after a fix drops the fixed key; the verdict then needs no entry for it.
- Tool descriptions and docs contain the review-with-evidence framing (multi-substring test, mirror the T-0098 pattern).
- Existing verdict tests from the two NEEDS tasks keep passing with the amended semantics (adjust only the not-waivable fixtures, list each adjustment in the report).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Red-run: the unaddressed-findings refusal test written first, shown failing against the freshly merged verdict code; paste the failing output.
CROSS-VERIFICATION (orchestrator, after done): an independent verifier with a distinct agent identity performs one real review on a live draft: waives one finding with a reason, fixes another, records the verdict, confirms the gate honors it; from the diff alone. Verdict in the archive note.

SCOPE: the verdict paths in grill.go and validate.go, the gate in tools.go, the one EARS rule, docs wording, tests. Do not change the computed classes themselves, the identity/hash binding, the reopen machinery, or the edge-commit engine.
ROLLBACK: revert the commit; the EARS rule retires via rule op=retire. Waiver records in journals are inert history for a reverted server.
REPORT BACK: the finding-key scheme as landed, each adjusted fixture, the real waive-and-approve transcript, each test's result including the red-run, anything deliberately not done.

## T-01KYD9RJTREBEVQFV34HYW8VJ2 redundancy findings in the validation pack: diff-scoped duplicate-block detection against the graph, so implementations reuse instead of re-writing
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD9466KEPWBV2RBK7EQM202
refs: R-0007
grilled: 2026-07-25
targets: internal/evidence, internal/mcpserver/validate.go, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS, in order: the validation-phase task (title: validate: the post-implementation phase) must be MERGED first - this task adds a section to its pack. The evidence-sweeps task (title: evidence sweeps scoped to an item's targets) should be merged first too - this task extends the internal/evidence package it creates. The verdict-authority task (title: steps are judgments, automations are implications) defines the waiver semantics these findings inherit; no code coupling, but read it.

WHY. The standing requirement: implementations must be free of redundant blocks that could and should be reused. The cost in this project is not abstract style - it is measured in agent tokens, and this repository carries the proving example: gate[T] (tools.go:175-185) and the rule tool's inline twin (tools.go:200-210) are the same block written twice, and every task brief that touches the tool dispatch has had to say "and the rule tool's inline twin" forever after - duplication makes every future brief longer, every lease broader, and every fix a two-site fix that B-0003-style divergence eventually splits. Prose review does not catch this reliably (the twin survived every review to date); a computed finding in front of the validator will.

PLACEMENT DECISION, alternatives rejected: the detector runs in the VALIDATION pack, scoped to the item's diff. Rejected: a check-time global sweep (unbounded output and cost, violates the token principle; a whole-repo duplication census is a one-time study, draftable as an R-item, not a standing gate); a grill-time check (pre-implementation, there is no new code to compare); a CI linter like dupl bolted outside the loop (its findings would reach no verdict, no waiver record, and no implementer brief - orphaned evidence).

VERIFIED GROUND (do not re-derive)
- The graph stores KFunc/KMethod/KKernel nodes with file and line spans (internal/graph); the indexer caches per-file parses keyed by content hash with a CacheVersioner version salt (internal/index, the T-0127 pattern) - the fingerprint index MUST reuse that cache discipline, including a version salt so detector changes invalidate stale fingerprints.
- internal/evidence exists after the NEEDS task: pure functions over graph + file access, records render one line each, caps + tail, deterministic order. Extend it; do not create a second evidence home.
- The validate pack's sections, budget, and finding-key/waiver semantics come from the two NEEDS tasks. A dup finding is a normal computed finding: stable key dup:<new-node>, addressable as fixed or waived-with-reason per the verdict-authority semantics - deliberate duplication (a fixture, a fork-on-purpose) is the validator's judgment to record, never the server's to guess.
- MCP-005 is the in-repo precedent for similarity mechanics: sentence-token Jaccard >= 0.6 for rule merge suggestions, suggestion-only. Same philosophy here: the server surfaces, the agent judges.

WHAT TO BUILD
1. FINGERPRINTS: internal/evidence gains Fingerprint(node span bytes) and an index built lazily per validation run over the graph's function-kind nodes, cached under the workspace cache dir keyed by (file content hash, detector version). Normalization for type-2 clones: Go tokens via go/scanner with identifiers and basic literals replaced by kind placeholders; non-Go spans fall back to whitespace-normalized byte shingles (state the shingle size as a const with rationale). Minimum block size: 15 significant tokens (below that, matches are idiom, not redundancy) - const with rationale.
2. DETECTION, diff-scoped: for each function-kind node ADDED or CHANGED in the item's diff (the validate pack already computes the changed-file set), compare against the index; report best matches with similarity >= 0.85 as "v dup <new-node> ~= <existing-node> <pct>" - capped 10 + tail, deterministic order, first 1 match per new node (the best; more is noise). Matches WITHIN the diff (two new functions duplicating each other) count and are reported the same way. Test files compare only against test files, production only against production (fixture boilerplate across tests is a different, deliberate idiom - state this in a comment).
3. THRESHOLDS are consts with one-line rationales, and the proving example is the calibration fixture: the gate/rule-inline twin pair MUST score above threshold in a test pinning real excerpts of both blocks (copy them into the fixture; do not read tools.go at test time - the twin is scheduled for de-duplication by the edge-commit work and the fixture must survive that).
4. COST CEILING, enforced: index construction touches only files the graph names for function nodes, is cached across runs, and the per-run comparison is O(changed nodes x index buckets) via shingle-hash buckets, never all-pairs. Add the pass to the validate timing test; SPX-MCP-001 (2s warm, 1 MiB reads) holds - the cold index build may exceed the read budget on huge repos, so it reads spans, not whole files, and reports "v dup-index truncated" past a 2000-node ceiling rather than blowing the budget. Report measured wall time cold and warm on this repository.
5. GUIDANCE (one sentence each, guidance layer only, per the enforcement layering): docs/agent-workflow.md's task-body anatomy gains: name the existing helpers the implementer must reuse (find scope=code before writing); the orchestrator checklist gains: a dup finding in validation means the brief failed to name a reusable helper - fix the brief pattern, not just the code.

NON-NEGOTIABLE PROPERTIES, each with a test
- The calibration pair (gate/rule-twin excerpts) scores >= threshold and is reported; an unrelated pair scores below and is not.
- A diff adding a function 90% identical to an existing one yields exactly one v dup finding with the correct pair; fixing by delegating to the existing function clears it on re-render.
- Two new functions duplicating each other are caught intra-diff.
- Test-vs-production isolation holds both ways (fixture proves both directions silent).
- Caps hold (30 duplicating functions -> 10 + tail); determinism (two runs byte-identical); cache: second run on unchanged files rebuilds nothing (assert via a counting hook or cache-dir mtime check - state which).
- A generated file (contains a standard generated-code marker line) is excluded from both sides, tested.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Run validate on a fixture item whose diff plants one true duplicate; paste the #dup section. Report cold and warm wall times on this repository.
  Red-run: the calibration-pair test written first, shown failing before the detector exists; paste the failing output.
CROSS-VERIFICATION (orchestrator, after done): an independent verifier with a distinct agent identity re-runs the calibration and isolation tests from the diff alone, then validates one real merged item and judges the findings (waiving any deliberate duplication with reasons); verdict recorded in the archive note.

SCOPE: internal/evidence extension, the #dup section wiring in validate.go, the two guidance sentences, tests. Do NOT de-duplicate any existing production code in this task (the gate/rule twin included - that is its own change with its own review), do not touch grill.go, tools.go, the graph, or the index beyond reading.
ROLLBACK: remove the #dup section call; the fingerprint code is dead until deleted; cached fingerprints are garbage-collected by the existing cache versioning. No stored state elsewhere.
REPORT BACK: threshold and shingle consts as landed with rationales, calibration pair score, cold/warm timings, the fixture #dup section verbatim, each test's result including the red-run, anything deliberately not done.

## T-01KYDA92TGF3FT994TEE6EDVN6 one task, one branch, one pull request: every finished task opens its own PR immediately, never batched
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD8HSZ0ERTBFBBEVQD68M4R
refs: R-0007, T-01KYDA17KBFV3SBCEV79KXW7Z3
grilled: 2026-07-25
targets: CONTRIBUTING.md, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Documentation-and-policy task, no Go code. Supersedes the rejected draft in refs: the user set the merge decision back to a human step - PRs open immediately but are NEVER auto-merged - and added the always-pushed rule; both are folded in, everything else re-recorded intact.

NEEDS: the merge-policy task (title: merge policy: never squash) must be MERGED first - both rewrite the same CONTRIBUTING section and this task extends the policy that one establishes. Do not run concurrently.

WHY. The decision-trail principle has three granularities and the third was convention, not policy: edge commits make every state transition visible inside a task; never-squash merges keep those commits readable on main; and PR-per-task makes the task itself the unit of review and merge. Batching several finished tasks into one pull request re-creates at the PR level exactly what squashing creates at the commit level - N decisions flattened into one reviewable blob, reviewers approving work they cannot attribute, and a revert that cannot take one task back without taking its neighbors. This session's own history is the proving case: multiple finished work items accumulated on one branch and shipped in combined PRs, so no single task can be reverted, bisected, or re-reviewed in isolation.

THE POLICY, exact: when a task reaches done, its branch is pushed and a pull request is opened IMMEDIATELY - before the next task starts, not at end of session. One task per PR; the PR title carries the full task ID. NO AUTO-MERGE: merging is a human decision, a judgment step per the steps-are-judgments principle - the agent drives CI to green on the open PR (diagnose and push fixes on red) but never merges and never arms auto-merge; when it merges by hand on instruction, the method is merge commit, never squash (sibling policy). ALWAYS PUSHED, ALWAYS COVERED: at no point may changes exist that are not pushed and not covered by an open PR or draft PR - work in progress lives on its pushed branch under a DRAFT pull request from the first commit, flipped to ready when the task is done. An unpushed local change is invisible to every other agent and survives no container; a pushed branch without a PR is work nobody can find from the review surface. Both states are forbidden at any time, not just at task end. Work that never was a task (a typo fix in passing) rides with the task that touched it or becomes a task if it stands alone - nothing merges outside a PR either way, which CONTRIBUTING already mandates. The swarm's worktree branches (spectackle/<item-id>) already give every task its own branch; this policy makes the PR boundary follow the branch boundary instead of collapsing branches into a shared integration branch first.

WHAT TO BUILD
1. CONTRIBUTING.md: the pull-request section gains the one-task-one-PR rule with the granularity rationale above compressed to a short paragraph, including the explicit anti-pattern (a session-accumulation branch carrying several finished tasks) and the exception handling (follow-up commits to an OPEN unmerged PR for the same task are fine; a merged PR is finished and follow-ups are a new task, which the section already states for branches).
2. docs/agent-workflow.md: the orchestrator's git duties gain: open a draft PR with the first pushed commit of a task, flip it to ready and stop pushing when the task is checked and done, drive CI to green, leave the merge to the user; never hold finished work hostage to unfinished siblings, never leave any change unpushed or uncovered. One sentence on the payoff: revert, bisect and review all regain task granularity.
3. Consistency sweep: no remaining sentence in either file implies batching finished work or opening PRs at session end (grep-based check as in the sibling task, state the method).

NON-NEGOTIABLE PROPERTIES
- The CONTRIBUTING section, read cold, answers: when is the PR opened (draft at first push, ready at done), what does it contain (one task), what is in the title (the full task ID), who merges (the user, by hand, merge-commit method), and what may never exist (unpushed changes; pushed branches without a PR). A verifier answering from the text alone is the test, mirror the sibling task's approach.
- No contradiction with the never-squash section or the auto-merge prerequisites it documents - the sections must read as one coherent policy.
- go build ./... and the full suite still pass (docs must not break anything embedded).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  grep -n batch CONTRIBUTING.md docs/agent-workflow.md - paste; hits must be the anti-pattern text itself.
CROSS-VERIFICATION (orchestrator, after done): independent verifier reads both sections cold and answers the four policy questions from the text alone; verdict recorded in the archive note.

SCOPE: the two named markdown files. No Go code, no templates (the backward-path task owns them - if its workflow step 8 wording conflicts once both land, the LATER merger reconciles and says so in its report).
ROLLBACK: revert the commit.
REPORT BACK: the final CONTRIBUTING paragraph verbatim, the grep output, any reconciliation with the sibling tasks' text, anything deliberately not done.

## P-01KYDF59RYEVYRDADEP65GF2ME total decoupling: the LLM issues no git or forge command, and every one is mechanical in the server
kind: proposal
state: active
created: 2026-07-25
refs: ADR-01KYDBQGMRFBN9SAHCWWNSAKX4
grilled: 2026-07-25
targets: internal/mcpserver, internal/coord, internal/wt

Requirement: the LLM does spec-driven thinking and nothing else. Every git and forge action is a mechanical consequence of a state transition, performed by the server. Pull request titles are generated by the server; pull request bodies are generated from the task, the change, and whatever triggered the commit. Parallel multi-agent flows are first-class, not an afterthought.

WHAT IS ALREADY MECHANICAL, measured against the running implementation rather than assumed: branch creation, code commit, push, draft pull request open, ready-on-done, merge-on-archive, and the pull request title and body — the title is composed from the item ID and title, the body from the item brief plus closing keywords for cited issue URLs. None of that costs the LLM a command today.

WHAT IS NOT, and this is the entire gap. Every remaining git action an LLM still performs in practice is one of four:
1. Committing the .spectackle records. CommitCode excludes them deliberately (B-0006, preserveSpectackle), so after every transition the record delta sits uncommitted and a human or an LLM commits it. This is the largest of the four and the one that makes the decoupling claim false today.
2. Waiting for CI and merging afterward. gitFlowMerge merges on archive with no notion of whether the branch is green, so the LLM watches CI and sequences the archive by hand. ADR-01KYDB names task verification as the gate; there is currently no gate.
3. Integration housekeeping: fetching, merging the base branch into a stale task branch, and resolving record conflicts when a sibling lands first.
4. Rebuilding and restarting the resident server after a merge, which CONTRIBUTING already mandates and nobody automates.

THE CHAIN, and why it cannot be short-circuited. Item 1 is the edge-commit engine, already specified as a task, and that task NEEDS coord.db serialization of server-side whole-file rewrites — because two server processes on one checkout race the git index exactly as they race work.md. With parallel agents that is not a theoretical race: it is the normal operating mode of this repository, where four implementers routinely run at once. Committing records without that serialization would turn a rare lost journal write into a corrupted index. So the order is forced: serialize first, then commit edges, then gate the merge on verification.

PARALLEL FLOWS AS A FIRST-CLASS CONSTRAINT, not a caveat. Every mechanism here must hold when N agents work at once: one branch and one pull request per task (already true — the branch name derives from the item ID), no shared integration branch, leases proving scope disjointness before work starts, and the record commit serialized cross-process. A design that only works single-threaded is a design that fails on this repository's own workflow.

REJECTED: having each agent commit records opportunistically without a lock (the race above); a single shared branch that agents append to (it is the batching anti-pattern the PR policy already forbids, at branch granularity); and making the LLM poll CI (that is the command we are removing, not relocating).

EXIT CRITERION: a full task lifecycle — draft through archived, with a sibling agent landing a conflicting change midway — completes with the LLM issuing zero git and zero forge commands, and the resulting main branch carries one merge commit per task whose pull request body names the task, the change and the trigger.

## ADR-01KYDGXWH4FX9VQTG0G2CF8GQQ May the git automation ever suppress an action silently?
kind: adr
state: done
created: 2026-07-25
context: The first gitflow design stayed quiet when the feature was disabled, the workspace was not a repository, no remote existed, or a step had nothing to do — citing the output diet. Two real defects then hid in exactly that ambiguity: a pull request silently still-draft after a claimed ready, and a config file silently never committed across a whole lifecycle. A quiet non-action is indistinguishable from a broken one.
decision: never-silent: every suppressed or deferred git action emits one dense record naming the reason
consequences: Every gate refusal, deferral and no-op in the git automation names itself in one record: off-by-config, swarm-worktree deferral, not-a-repository, no-remote, pr-deferred-until-first-commit, up-to-date, records-clean, merge-skipped. The output diet still governs everything else on the surface; the exception is scoped to the automation's own actions, where this session showed the ambiguity between off and broken hides real defects. Costs a handful of short g records per transition.
status: accepted

kind: radio
option: never-silent: every suppressed or deferred git action emits one dense record naming the reason
option: diet-first: keep omit-if-empty; suppressed actions stay silent
choice: never-silent: every suppressed or deferred git action emits one dense record naming the reason

## P-01KYDPRXQSF8XAP1HCHY7390T8 every MCP text earns its tokens: a benchmark harness scores hint and record variants by tokens against validity, judged by independent agents over tricky fixture repos covering all eight states
kind: proposal
state: active
created: 2026-07-25
refs: ADR-01KYDGXWH4FX9VQTG0G2CF8GQQ
grilled: 2026-07-25
targets: internal/mcpserver, internal/bench

Requirement: improve every hint and text the MCP emits so the state-machine workflow reaches the best result with minimal tokens — valid and complete above all. Implementations compete against each other in a deliberate benchmark, preferably driven by independent agents over example repositories with tricky outputs covering every state of the machine. Gaps and bugs found along the way are recorded, and the server is updated after every task.

BASELINE, measured on the live server this hour: the instructions manifest is 7238 bytes, one state call on this repository answers 5605 bytes, and every tool result may carry piggybacked hints. These texts are read by an LLM on every single call, so their cost is multiplied by the call count of every session — and their QUALITY decides whether the agent takes the right next transition or wanders. Token cost and outcome quality pull against each other, which is exactly why variants must be measured, not argued.

THE HARNESS, the first deliverable and the precondition for everything else.
1. Fixture repositories, generated not hand-kept: small synthetic workspaces whose records and code deliberately exercise every state — draft through archived, rejected with revocation, blocked with all three escalation exits — plus the tricky output shapes that have bitten this session: ambiguous short-ID prefixes, ID-dense state output, refusal grammar, degraded-index findings, gitflow records in offline mode.
2. A driver that walks a scripted full lifecycle over a fixture through the REAL tool surface (offline mode, no network) and captures every tool result byte. Metrics per run: total result bytes and estimated tokens, per-tool breakdown, and a VALIDITY verdict computed from the workspace afterward — end states correct, no dangling references, check ok — plus a COMPLETENESS check that every record the workflow promises actually appeared. Invalid runs never count as wins regardless of token savings.
3. A/B comparison: the same script over the same fixture against two server builds, reported as tokens-delta and validity-delta. This is how any future text change proves itself; a variant that saves tokens and loses validity loses.
4. Independent agents as judges, second stage: fresh subagents each driving a fixture through one server build with a fixed task brief, scored on whether they reach the correct end state and how many tokens of tool output they consumed. Agents are the only honest judge of GUIDANCE quality — a scripted driver cannot get lost, an agent can.

REJECTED: optimizing texts by taste without the harness (this session already shows dense-looking texts hiding real ambiguity), and token-only scoring (validity outranks tokens by construction).

EXIT CRITERION: the harness runs in CI-free local mode; a baseline report exists for the current texts; at least one text improvement lands that the benchmark shows strictly better — fewer tokens at equal validity or better validity at equal tokens — and every state of the machine appears in at least one fixture run.
