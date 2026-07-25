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

## B-01KYD1G9G1EVCAEWWVFR15GRT3 rule op=edit silently discards the edit and answers ok when the pattern slot is omitted
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/tools.go, internal/spec/author.go

GitHub issue 25. Reported from field use of the released binary while migrating an external spec bundle: fifteen consecutive rule op=edit calls all answered ok and all were no-ops, discovered only by re-reading the file.

OBSERVED: omitting the pattern slot on edit writes nothing, raises nothing, and returns the success record. Worse, the W001 lint finding printed alongside is computed against the OLD text, so it describes a rule state that exists neither before nor after the call.

ISOLATED CAUSE (from the report, verify before fixing): the edit path is gated on pattern being non-empty. With it empty the EARS recomposition branch is skipped entirely, yet control still falls through to the success return. The slot validation that would have caught the incomplete set sits BEHIND that branch, which is why a pattern-bearing call with missing slots correctly refuses while a pattern-less one silently succeeds. system and response alone are also insufficient. The error channel itself is healthy: an unknown rule ID fails correctly.

WHY IT IS THE WORST OF THE SIX: the tool contract is that all writes go through tools and the caller never edits these files. An agent following that contract has no independent way to verify a write landed, so it trusts ok. A silent no-op converts directly into spec drift that surfaces much later, with a stale lint line arguing the old text is current.

FIX DIRECTION: either apply the edit using the stored pattern, which makes partial edits work, or fail loudly like the sibling paths. Decide which, and note the reporter's cheap detection heuristic: add echoes a composed r line and a no-op edit does not, so the echo's presence is a usable success signal even before the fix.

VERIFY: regression test over the reproduction — edit without pattern must either apply or refuse, never answer ok unchanged; the lint finding accompanying an edit must be computed against the text actually stored.

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
state: draft
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
