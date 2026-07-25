---
schema: v1
---

## ADR-01KYCZ13KRF84VD5DSVQ4017MV Which ID scheme closes the cross-clone collision hole?
kind: adr
state: done
created: 2026-07-25
context: Verified: coord.NextID is serialized per machine, but coord.db is gitignored, so the counter is per-clone state. Two clones drafting concurrently mint the same ID, and replay reconciles items by upserting under their ID — rules have a collision remap path, items do not, so two different items silently collapse into one. Measured cost of full 26-char IDs: about 11 percent more output on a state call (1718 bytes, 10 ID occurrences), against an explicit output-diet contract.
decision: short-prefix: store the full UUIDv7 base32 ID, display and accept a short unique prefix like git shas
consequences: Records carry a globally unique, time-ordered ID, so the cross-clone collision hole closes by construction and replay needs no item remap. Displayed and accepted form is the shortest unambiguous prefix, keeping ID-dense output close to today. Costs a resolver plus an ambiguity path at every tool boundary that takes an ID. One caveat drives the prefix length: UUIDv7 puts a 48-bit millisecond timestamp first, which is about ten base32 characters, so records minted seconds apart share a long leading run and the prefix must be chosen adaptively rather than at a fixed short length. Legacy sequential IDs stay resolvable, since archived records live on as journal tombstones.
status: accepted

kind: radio
option: short-prefix: store the full UUIDv7 base32 ID, display and accept a short unique prefix like git shas
option: full-id: kind-prefixed full 26-char ID everywhere, simplest to implement and read
option: remap-only: keep sequential IDs, extend replay rule-remap to items, no migration and no output growth
option: discriminator: sequential ID plus a per-clone suffix, unique but not globally ordered
blocks: P-01KYCZ04BRFJF9AH75QRWMGXPC
choice: short-prefix: store the full UUIDv7 base32 ID, display and accept a short unique prefix like git shas

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

## T-01KYD2XQG6E38APSR3EY4GY137 rule op=edit: recompose from the stored pattern instead of silently rewriting the old text, and stop eating the separator
kind: task
state: done
created: 2026-07-25
targets: internal/spec/author.go, internal/mcpserver/tools.go

Fixes GitHub issues 25 and 30 together, because both live in spec.EditRule and one of them is not where the reporter thought.

CORRECTED CAUSE FOR ISSUE 25 — the reporter's hypothesis was that the success return is reached without a write. It is not. Read the code: ruleEdit composes a sentence only when Pattern is non-empty, so a caller supplying system and response without pattern passes sentence="" into spec.EditRule; EditRule then does `if sentence == "" { sentence = old.Text }` and rewrites the block with the OLD text. The write genuinely happens, Written is true, and ok is truthful about the write while being a lie about the edit. The `! REJECTED E - nothing was written` branch exists and is simply never reached.

That fallback is deliberate, not an oversight: rationale and applies fall back to their stored values the same way, so EditRule was designed for partial edits. The defect is that the tool layer cannot supply a sentence without a pattern, so the one slot a partial edit most needs is the one that silently degrades it.

FIX DIRECTION FOR 25: when any EARS slot is supplied but pattern is absent, recover the pattern from the stored rule and recompose, which is what the partial-edit design already implies; refuse loudly only if the stored rule's pattern cannot be recovered. Do NOT simply make a missing pattern an error — that would break the legitimate rationale-only and applies-only edits the fallbacks exist for. Check whether the pattern is stored on the rule or must be re-derived from its text, and say which in the report.

ISSUE 30, same function: EditRule rebuilds the block as head, sentence, and optionally a blank line plus Rationale, then splices it between lines[:start] and lines[end:]. The reconstructed block carries no trailing blank line while the replaced span consumes the separator before the next rule, so every edit eats one separator permanently. add and edit therefore produce different bytes for identical content.

FIX DIRECTION FOR 30: make the serializer emit one canonical layout regardless of how the rule reached its text, so add and edit converge. Consider whether lint should assert canonical layout, since a formatting invariant nothing checks will drift again.

STALE LINT LINE, also issue 25: res.Findings is computed from the sentence variable AFTER the fallback substitution, so a pattern-less edit lints the old text and prints a finding describing a state that exists neither before nor after the call. Whatever the fix, the finding must describe the text actually stored.

SCOPE: internal/spec/author.go and its tests, plus internal/mcpserver/tools.go and its tests for the tool-layer half. BLOCKED-ON: T-01KYCZ6Q9RE98BBJECVK0M7GN8 currently holds tools.go; start with author.go only if that lease is still open, or wait.

VERIFY: the issue-25 reproduction — edit with system and response but no pattern — must change the stored text or refuse, never answer ok unchanged; the accompanying lint finding must be computed against the stored text; the issue-30 reproduction — three added rules, then edits to the first and second — must leave a file byte-identical to one where the same three rules were added with their final text directly; rationale-only and applies-only edits must keep working, with a test each, because they are what the fallbacks exist for.

ROLLBACK: one composition path and one serializer layout; both revertible independently.

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

## T-01KYD5JB7GF34BFNVRAAX8B6GE serialize server-side whole-file rewrites through the coord.db lock table
kind: task
state: draft
created: 2026-07-25
parent: B-01KYD57FN3ERHBM5EQ3534YJXP
targets: internal/coord/coord.go, internal/item/item.go, internal/spec/author.go, internal/drift/drift.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

DECIDED, do not re-open: the serialization point is coord.db, not an advisory file lock. Rationale: coord.db already is the cross-process serialization point and already holds leases, so this keeps one lock hierarchy instead of introducing a second one that some later change takes in the wrong order.

VERIFIED GROUND (the orchestrator read this; do not re-derive)
coord.DB already has exactly the mechanism needed and uses it for one purpose. LockIntegrate/UnlockIntegrate at internal/coord/coord.go:562-597 take a row in a `locks` table whose schema is already generic - it keys on a `name` column and stores agent plus an expiry, and an expired lock counts as free so a crashed holder cannot wedge the workspace. Today the only name ever used is 'integrate'. Generalizing it is a rename plus a parameter, not a new subsystem.

THE DEFECT (measured, see the parent item)
item.Upsert reads all of work.md via LoadWork, splices one item into the slice, and rewrites the whole file via writeWork at internal/item/item.go:406. Two concurrent writers both read N and both write N+1, so the later write erases the earlier one's record. Server.mu is per process and the shipped topology is N stdio processes, so it provides no mutual exclusion. Measured: 16 acknowledged drafts, 15 on disk repeatedly, 10 on disk once.

SAME SHAPE, ALSO IN SCOPE - each is a whole-file rewrite with no lock
  internal/item/item.go:406        writeWork, reached by Upsert AND Remove
  internal/spec/author.go:136,182,205,261,274   rule add, edit and delete
  internal/drift/drift.go:129      anchors.tsv rewrite

EXPLICITLY NOT IN SCOPE, and here is why, so you do not add a lock it does not need: journal.Append at internal/journal/journal.go:90 opens with O_APPEND|O_CREATE|O_WRONLY and writes one marshalled event. A single O_APPEND write under PIPE_BUF is atomic on POSIX, so concurrent appends interleave by record and never by byte. Leave it alone. If you believe otherwise, say so in the report with the evidence rather than changing it.

WHAT TO BUILD
1. Generalize the integrate lock into a named lock on coord.DB. Keep LockIntegrate working, as a thin caller of the general form, so the submit path is untouched.
2. Give it a scoped-execution wrapper that acquires, runs a closure, and releases on every exit path including panic. A bare Lock/Unlock pair invites the missing-defer that reintroduces this class.
3. Take the lock around the ENTIRE read-modify-write, not around the write alone. Locking only writeWork leaves the race exactly where it is: both readers still read the stale N.
4. Lock granularity is per context dir, not global. Writes to different bundles must stay parallel; a global lock would serialize the whole swarm behind one file. The lock name must therefore include the context.
5. Decide and state where the lock is acquired. The item and spec packages do not currently know about coord. Either they take a small interface the server supplies, or the server wraps the calls. Pick one, apply it consistently to all four writers, and justify it in the report - do not do it one way in one package and another way elsewhere.

NON-NEGOTIABLE PROPERTIES, each with a test
  No lost update: N concurrent writers, N records on disk, exactly.
  Crash safe: a holder that dies leaves a lock that expires, so the next writer proceeds. This already holds for 'integrate'; prove it still holds for the general form.
  No deadlock against leases: a code path that holds this lock must never block on a lease, and vice versa. State the ordering you established.
  Parallel across contexts: two writers on different context dirs must not serialize. Show it.
  No lock in the read path: get, find and state must not acquire it.

TESTS
  The acceptance test already exists: TestConcurrentDraftsPersistEveryItem in internal/mcpserver/swarm_test.go, currently skipped with a pointer to the parent item. DELETE THE SKIP - do not rewrite the test to match your implementation. It must pass unchanged, and it must be run enough times to mean something: -count=20 at minimum, since the pre-fix failure rate was roughly 2 in 20 and a single green run proves nothing.
  Add the same shape for item.Remove, for concurrent rule writes through spec authoring, and for concurrent anchors rewrites.
  Add a test that two different context dirs write concurrently without serializing.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go test ./internal/mcpserver/ -run TestConcurrentDraftsPersistEveryItem -count=20 ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root>   POSITIONAL path; the -root flag form is a known trap
  spectackle call -root <worktree-root> check '{}'   must end with a line that is exactly ok

SCOPE: the coord lock generalization plus the four writers named above. Do not change the ID scheme, the tool boundary, or the migration - those are separate lines of work.
ROLLBACK: the wrapper is one function; removing its calls returns to today's unlocked behavior. Say in the report whether any caller became structurally dependent on the lock beyond mutual exclusion.
REPORT BACK: where the lock is acquired and why there, the lock naming scheme and its granularity, the ordering you established against leases, the -count=20 result verbatim, each test's real result, and anything deliberately not done.

## P-01KYD8DK52EG5A1AKC1KSRR4Z2 close the four open field-reported defects: gitignore, worktree root, degraded index, stale hint
kind: proposal
state: active
created: 2026-07-25
grilled: 2026-07-25
targets: internal/index, internal/workspace, internal/mcpserver

Requirement: fix GitHub issues 26, 27, 28 and 29, in parallel where scope allows. All four were reported from field use of a released binary against real repositories, and none reproduce in this repository's own development checkout, which is why all four survived their own test suites.

WHY ONE PROPOSAL AND NOT FOUR: the four share a single root cause at the level that matters for planning. Every one of them is a behavior that is correct in the perspective it was written in and wrong in the perspective it is used in. Issue 26 walks a tree that is clean here and polluted there; issue 27 resolves a root correctly for a checkout and wrongly for a nested worktree; issue 28 reports a graph that is complete here and degraded there; issue 29 gives advice that is actionable here and impossible there. Grouping them keeps that lesson in one place and lets the exit criterion be stated once.

SCOPE PARTITION, chosen so four implementers can run concurrently. The file sets are disjoint except for internal/workspace/workspace.go, which two tasks touch in entirely different functions (Root.SkipDir versus the root-detection walk), so their hunks merge without conflict.
  26 -> new internal/ignore package + one call from Root.SkipDir
  27 -> the root-detection walk in internal/workspace/workspace.go
  28 -> internal/index/typespass.go, internal/index/indexer.go, internal/mcpserver/state.go, internal/mcpserver/tools.go check path
  29 -> internal/mcpserver/swarm.go
Record writes stay with the orchestrator: implementers touch code and tests only, never .spectackle, so four parallel agents cannot collide on work.md or the journal. That collision is real and was paid for once already this session, when two open PRs both had to be hand-merged on .spectackle/spec.md after a sibling landed.

DECISIONS TAKEN BY THE ORCHESTRATOR rather than deferred, each recorded on its task with the reasoning: ask git for gitignore semantics instead of reimplementing them; a .git file terminates the root walk exactly as a directory does; index degradation becomes a first-class record on state and check; the stale-binary hint fires only when the running binary is a development build of spectackle serving spectackle's own tree.

REJECTED ALTERNATIVES. Reimplementing gitignore matching in Go: negation, directory-only patterns, nested .gitignore files, .git/info/exclude and the global excludes file are more semantics than a prefix match, and getting them subtly wrong would silently exclude real code from the graph, which is worse than the inflation being fixed. Making the existing ignore and ignore_regex config knobs the answer to issue 26: that inverts the default, so every new checkout gets a polluted graph until its operator enumerates their own ignore list a second time. Suppressing the stale hint behind a config flag: a noisy default that each user must silence is the defect, not the fix.

EXIT CRITERION: all four GitHub issues verifiably closed against a reproduction rather than an argument; go test ./... -race green; check ok; and for issue 26 a measured before/after file count on a tree containing a gitignored copy, since the report is a measurement and the fix must answer it in the same terms.

## T-01KYD8EDGCEGJSCAEEVYKJ0VRY honor gitignore during the index walk, by asking git
kind: task
state: active
created: 2026-07-25
parent: P-01KYD8DK52EG5A1AKC1KSRR4Z2
refs: B-01KYD1G9J5EHBBT823EK0MGT3T
targets: internal/ignore, internal/workspace/workspace.go

Closes GitHub issue 26 (see B-01KYD1G9J for the field report and its measurement).

DECISION ALREADY TAKEN, do not relitigate: ask git, do not reimplement gitignore. Negation, directory-only patterns, nested .gitignore files, .git/info/exclude and core.excludesFile are more semantics than a prefix match, and a subtly wrong matcher would silently exclude real code from the graph, which is worse than the inflation being fixed.

WHAT TO BUILD
1. New package internal/ignore. One exported constructor that takes a workspace root and returns a matcher, plus a method answering whether a repo-relative slash path is ignored. Populate it with ONE batched git invocation per construction, not one per path: `git -C <root> check-ignore --stdin -z` fed the candidate paths, or `git -C <root> ls-files --others --ignored --exclude-standard --directory` — pick from what you can verify, and say in your report which you used and why.
2. Not a git repository, git absent from PATH, or git failing for any reason: the matcher must answer false for everything, so a non-git workspace behaves exactly as it does today. This is a degradation to current behavior, never an error on a tool call.
3. Wire it into workspace.Root.SkipDir (internal/workspace/workspace.go around line 311), after the existing defaultSkipNames and Cfg.Ignore checks so config still wins and the cheap checks run first. STAY INSIDE SkipDir: a sibling task is editing the root-detection walk in the same file and your hunks must not overlap.
4. Construct the matcher once per walk, not per call to SkipDir. SkipDir is called for every directory entry; a subprocess per entry is unacceptable. Cache it on Root or pass it through — your call from reading the code, but state the shape and its lifetime in your report.

COST CONSTRAINT: one subprocess per index at most. If you cannot get that with the batched form, report the number you did achieve rather than shipping a per-path spawn.

TESTS
  a fixture repo with a real .gitignore listing a directory: that directory's files are absent from the walk, and a non-ignored sibling with the same symbol name is present and owns the unsuffixed node ID.
  a negation pattern (!keep.go inside an ignored dir) is respected, which is the case a prefix matcher gets wrong.
  a nested .gitignore deeper in the tree is respected.
  no git repository at all: every path is walked, byte-identical to today.
  git unavailable (simulate via PATH or an injected runner): same, and no error surfaces.
  the config ignore/ignore_regex knobs keep working and keep taking precedence.

VERIFY: go build ./... ; go test ./internal/ignore/... ./internal/workspace/... -race ; go test ./... ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL path). Then MEASURE, because the report is a measurement: build a tree with a gitignored copy of a package, index it before and after your change, and report both file counts.

SCOPE: internal/ignore (new, with its own *_test.go per SPX-TST-001) and the single SkipDir call site. Do NOT touch root detection, internal/index, internal/mcpserver, or any .spectackle file.
ROLLBACK: remove the SkipDir call; the package becomes dead but harmless.
REPORT BACK: which git invocation, the matcher's lifetime, the before/after counts, each test's real result, anything deliberately not done.

## T-01KYD8F5WGFMCAFHFEGREFNRPS a .git file terminates the root walk, so a nested worktree is its own workspace root
kind: task
state: active
created: 2026-07-25
parent: P-01KYD8DK52EG5A1AKC1KSRR4Z2
refs: B-01KYD1G9KQF87REB16T0AXRDYP
targets: internal/workspace/workspace.go

Closes GitHub issue 27 (see B-01KYD1G9K for the field report and the three probes that pinned the rule).

DECISION ALREADY TAKEN: a .git FILE marks a git worktree root and must terminate the upward walk exactly as a .git DIRECTORY does. The reporter named the opposing reading — that centralizing the bundle at the main checkout may be deliberate, since work op=start manages worktrees itself and worktrees_dir defaults inside the repository. It is rejected for this reason: an explicit -root naming a directory must never silently resolve somewhere else, and several coding harnesses place agent worktrees inside the repository, so under today's behavior every such agent writes to the shared main checkout's bundle regardless of what it passes. That is precisely the collision the lease and swarm design exists to prevent.

VERIFIED GROUND, do not re-derive. internal/workspace/workspace.go line 107 decides the git marker with dirExists(filepath.Join(dir, ".git")), which is what skips .git files. A helper that stats .git regardless of its type ALREADY EXISTS in the same file around line 266 (see IsNestedGitBoundary and its comment, which explicitly names the file case). So this is a matter of using the right predicate at line 107, not writing a new one.

THE PART THAT NEEDS CARE, and where this task can break the swarm: workspace.Detect(start, root) resolves the MAIN repository separately from the active root — a linked worktree deliberately resolves to its parent for COORDINATION (coord.db, leases, agents live in the main repo's cache). Do not collapse those two. Read Detect and the mcpserver reroot path before changing anything, and keep main resolution behaving exactly as it does today; only the ACTIVE root gains the ability to be a nested worktree.

TESTS
  a git worktree nested inside its main checkout, given as -root, gets its own .spectackle bundle, and the enclosing checkout's bundle is untouched.
  a worktree OUTSIDE the main checkout keeps working (it already did).
  no git anywhere above: the workspace still anchors at the given directory rather than walking to the filesystem root.
  -root pointing at a different repository still targets that repository.
  main-repo resolution is unchanged: from inside a nested worktree, the resolved MAIN root is still the parent checkout, so coordination state stays shared.
  work op=start's own worktrees continue to resolve as they do today — assert it, do not assume it.

AND ONE REPORTING FIX from the same issue: the path in the result is printed relative to the resolved root, which hid where the write actually went and made the answer read as if it had landed locally. Make it unambiguous which root a reported path is relative to. Say in your report what you changed and why that wording.

VERIFY: go build ./... ; go test ./internal/workspace/... ./internal/mcpserver/... -race ; go test ./... ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL). Then drive the reproduction live: create a nested worktree with git worktree add, run the binary with -root pointing at it, and show which bundle the write landed in.

SCOPE: internal/workspace/workspace.go root detection only. A sibling task is editing Root.SkipDir in the same file — stay out of it. Do NOT touch internal/index, internal/ignore, or any .spectackle file.
ROLLBACK: one predicate at the marker check.
REPORT BACK: the predicate you used, what you verified about main resolution, the live reproduction, each test's real result, anything deliberately not done.

## T-01KYD8GDBKF3FTZN64KM733A5Y index degradation becomes a first-class record on state and check
kind: task
state: active
created: 2026-07-25
parent: P-01KYD8DK52EG5A1AKC1KSRR4Z2
refs: B-01KYD1G9PSEH5AQHAV7N4ZQ4BT
targets: internal/index/typespass.go, internal/index/indexer.go, internal/mcpserver/state.go

Closes GitHub issue 28 (see B-01KYD1G9P for the field report).

WHY THIS IS DANGEROUS RATHER THAN UNTIDY, and the sentence to keep in mind while choosing the record's wording: when the typed-call pass fails the graph silently degrades to syntactic-only, which removes exactly the capability the server's own instructions advertise as the reason to prefer get depth over shell search — cross-language impact radius, and what calls X. There are then no typed call edges at all, so an impact query returns a confident-looking but structurally incomplete answer, state still says ok graph with node and edge counts, and the caller has no signal to distrust the radius. An agent consulting get depth=2 before editing a symbol underestimates blast radius and never learns why. The only notice today is a line on reindex's stderr, which an agent driving the server over stdio, HTTP or the call subcommand never sees.

VERIFIED GROUND: internal/index/typespass.go returns the failure as an error from its load path (see the two fmt.Errorf sites, one of which already counts the failing packages and names an example). Today that error is logged and swallowed. The count and the cause are therefore already available at the point of failure — this task is about carrying them out to the surface, not about detecting anything new.

WHAT TO BUILD, two independent closures. Both are wanted; either alone leaves half the hole open.
1. Carry the degradation out of the index as state: whether the typed pass ran, and if it did not, the cause and the number of affected packages. Where that lives — a field on the index result, on the graph, or a small struct the server reads — is your call from reading the code; say which and why. It must survive to the server, which today only sees a graph.
2. Surface it on BOTH state and check as a record an agent can branch on, naming the cause and the affected package count. Do not invent a new record letter: reuse the existing dense grammar (docs/tools.md has the table; the ! finding and h hint shapes are both plausible, pick one and justify it). A healthy index must emit NOTHING extra — the output diet is a standing contract (SPX-ARC-002 and the omit-if-empty rule), so this must not become a line on every call.

AND the actionable diagnostic half: reindex already prints both Go versions when the toolchain mismatch is the cause. Make that a diagnostic the operator can act on — rebuilding with a newer Go is an immediate fix — rather than an incidental log line.

TESTS
  with a forced typed-pass failure, state emits a degradation record instead of a bare ok graph, and the record names the cause.
  the same for check.
  the record carries the affected package count.
  a healthy index emits nothing extra on either tool, asserted as an absence, so the output diet is preserved.
  the existing state and check output shapes are otherwise unchanged.

VERIFY: go build ./... ; go test ./internal/index/... ./internal/mcpserver/... -race ; go test ./... ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL). If docs/tools.md's grammar table gains anything, update it — SPX-REPO-001 binds the docs to the structs.

SCOPE: the three named files plus the check path in internal/mcpserver/tools.go and their tests. Do NOT touch internal/workspace, internal/ignore, internal/mcpserver/swarm.go, or any .spectackle file — three siblings are working those concurrently.
ROLLBACK: the surfacing is additive; removing the record restores today's silence.
REPORT BACK: where the degradation state lives and why there, which record shape you chose and why, each test's real result, anything deliberately not done.
