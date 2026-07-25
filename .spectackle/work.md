---
schema: v0
---

## P-0088 globally unique, chronologically sortable record IDs: close the cross-clone collision hole
kind: proposal
state: active
created: 2026-07-25
grilled: 2026-07-25
targets: internal/ids, internal/coord, internal/item, internal/replay

Requirement: reference IDs can be minted twice when several agents work in parallel; move to chronologically sortable global IDs (UUIDv7 rendered in a base32 alphabet) and migrate this repository's existing records.

PREMISE, VERIFIED AND CORRECTED
The stated failure mode is real but its cause is not concurrency between agents. Measured facts:
- coord.NextID runs inside an immediate-mode SQLite transaction with a retry loop, and coordination always resolves to the MAIN repository even when a server starts inside a linked worktree. Every agent on one machine therefore mints through one serialized counter. This is precisely the case that is already safe; a regression test for concurrent drafts exists.
- coord.db lives under .spectackle/cache/, which is gitignored and never committed. The counter is per-clone, per-machine state that no merge ever reconciles.
So the collision window is not agent-vs-agent, it is clone-vs-clone: two checkouts (two people, a fork, two CI runners) drafting concurrently both mint the same next number, and git merges the records without noticing.

THE HOLE IS DEEPER THAN THE REQUIREMENT STATES
replay.Run already anticipates ID collisions for RULES: applyRule detects an existing rule with different content, re-mints it and records the mapping in Report.Remap, then rewrites references. Items have no such path. Replay reconciles items by upserting them under their ID, so two genuinely different items that were minted the same ID in different clones silently collapse into one record, and the loser's body, state and history are overwritten. Rules were protected; proposals, tasks, bugs, research and ADRs were not.

COST, MEASURED NOT ESTIMATED
A UUIDv7 in a 32-character alphabet is 26 characters; with a kind prefix, 28. Current IDs are 6. On a live state call the output is 1718 bytes carrying 10 ID occurrences, so full-length IDs add about 11 percent to that surface alone, and find/get results carry proportionally more IDs. This repository's architecture contracts include an explicit output diet, so the trade-off is real and belongs in the decision rather than in a footnote.

OPTIONS (the open decision, see the linked ADR)
A. Kind-prefixed full ID everywhere. Maximum safety, chronologically sortable, roughly 11 percent more output on ID-dense surfaces, and every existing reference in prose, tests and docs changes.
B. Full ID stored, short unique prefix displayed and accepted, expanded on ambiguity, as git does with commit shas. Keeps most of the token economy, costs a resolver and an ambiguity path.
C. Keep sequential IDs and close the verified hole by extending replay's existing rule-remap to items. Cheapest by a wide margin, no data migration, no output growth; does not deliver chronological sortability, and depends on every cross-clone merge passing through replay.
D. Sequential IDs plus a per-clone discriminator. Uniqueness without length explosion, but IDs stop being globally ordered and gain a second field to explain.

Rejected outright: minting from a wall clock alone (collides within a millisecond across clones), and a central ID service (a network dependency for a tool whose whole point is local, git-native operation).

MIGRATION CONSTRAINT
Whatever wins, this repository's own records must migrate, and the earlier T-0094 rejection is binding precedent: a one-off data migration does not justify a permanent tool. The migration therefore ships as a throwaway path, not as a subcommand that lives forever. Archived items exist only as journal tombstones, so the migration must rewrite journal history rather than only work.md, which the architecture otherwise forbids the LLM from touching.

Exit criterion for the whole line of work: no two records can share an ID across independently minting clones, this repository's records carry the new scheme with every internal reference resolving, drift anchors and the FTS cache rebuild clean, and check returns ok.

## ADR-0013 Which ID scheme closes the cross-clone collision hole?
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
blocks: P-0088
choice: short-prefix: store the full UUIDv7 base32 ID, display and accept a short unique prefix like git shas

## T-0138 automatic schema-stamped migration: a workspace on the old ID scheme upgrades itself instead of being refused
kind: task
state: approved
created: 2026-07-25
parent: P-0088
refs: ADR-0013
targets: internal/workspace, internal/migrate, internal/spec

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

BLOCKED-ON: the internal/ids, internal/item and internal/mcpserver tasks under P-0088 must all be merged first.

WHY THIS IS NOT A THROWAWAY (and why T-0094's rejection does not bind)
T-0094 was rejected because a one-off rewrite of one repository's records does not justify a permanent tool. This is a different animal: an ID scheme change reaches every workspace every user of a released tool already carries. The schema stamp today answers a mismatch with a hard error stating there is no migration (see spec.Load and workspace detection). Shipping the new scheme without a migration path would therefore turn every existing workspace into an unreadable one, and the honest instruction to users would be to regenerate and lose their history.

VERIFIED GROUND (orchestrator read this; do not re-derive)
workspace.SchemaStamp is a single global stamp written into the frontmatter of every server-written bundle: item.Upsert writes it into work.md, spec authoring writes it into spec.md, and both spec.Load and workspace detection refuse a mismatched stamp with an explicit no-migration error. Rules keep their own ID scheme and are NOT re-identified, but they share the stamp, so their files are part of the migration surface even though their content barely changes.

WHAT TO BUILD
1. Bump SchemaStamp to the next version.
2. On load, a bundle carrying the previous stamp must be migrated in place instead of refused. Where exactly that hook belongs is your call from reading the code, but it must run before any reader can observe a half-migrated workspace, and it must cover every bundle in the cascade, not just the root.
3. The migration rewrites: item IDs in work.md, every ID reference inside bodies, parents, needs, refs and decide option records, journal history (archived records exist only as tombstones, so an unmigrated tombstone becomes unreachable), and the stamp in every file it touches.
4. Mint each migrated ID from that record's own creation timestamp in the journal, not from wall-clock-now: the new IDs are time-ordered, and minting from now would flatten the archive's chronology into the migration moment.

NON-NEGOTIABLE PROPERTIES, each with a test
Idempotent: a second run changes nothing and reports nothing.
Atomic per workspace: a crash mid-migration must leave either the old state or the new one, never a mix. Say in your report how you achieved that.
Recoverable: keep the pre-migration bundles retrievable, and document how a user gets back.
Deterministic: the same input workspace migrates to the same IDs on any machine, since the timestamps come from the journal rather than the clock.
Never hand-edited: the migration writes through the same server-side paths and its output must satisfy the same parsers.

TESTS
  a v0 fixture workspace migrates: every item resolves, every parent, need and ref resolves, archived tombstones resolve, check returns ok.
  running the migration twice reports zero changes the second time.
  a workspace already on the new stamp is untouched.
  a nested cascade with several context dirs migrates all of them, not only the root.
  determinism: migrating two copies of one fixture yields identical IDs.
  an aborted migration (inject a failure partway) leaves a workspace that still loads.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL path, see B-0010)
Then migrate a COPY of THIS repository's .spectackle tree and report before/after record counts, that check returns ok, and that a second run is a no-op. Do not migrate the live tree; the orchestrator does that once the path is proven.

SCOPE: the migration package plus the stamp constant and the load hooks it needs. Do not change the ids package, the item model or the tool boundary — those landed in the sibling tasks.
ROLLBACK: restoring the previous stamp constant and removing the hook returns to refuse-on-mismatch; already-migrated workspaces then need the retained pre-migration copy, which is why keeping it is a required property above.
REPORT BACK: where you hooked the migration and why there, how atomicity and recovery are achieved, the before/after counts on the repository copy, each test's real result, anything deliberately not done.

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

SCOPE: internal/spec/author.go and its tests, plus internal/mcpserver/tools.go and its tests for the tool-layer half. BLOCKED-ON: T-0136 currently holds tools.go; start with author.go only if that lease is still open, or wait.

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

## P-01KYD7QT8YE6PAT515BGPQ5VM4 review and validation are recorded independent verdicts: grill reviews the draft with feedback and a research path, a validation phase judges the implementation, both bound to reviewer identity
kind: proposal
state: draft
created: 2026-07-25
refs: R-0007, P-01KYD47GZ7FAMAGM4NEF0BQS8T, P-01KYD6VP6VE2Z8A517AT3RP39T
grilled: 2026-07-25
targets: internal/mcpserver/grill.go, internal/mcpserver/tools.go, internal/journal/journal.go, internal/lifecycle/lifecycle.go, docs/lifecycle.md

PROBLEM. Two review moments exist in the loop and neither is a review. Before implementation, grill renders a pack and stamps a date - the stamp records that rendering happened, not that anyone read the pack, closed its gaps, or fed anything back into the body; twelve grills in this repository's history changed zero bodies. After implementation there is no phase at all: the submit gate runs commands, check scans the workspace, and done rolls into archived on the orchestrator's say-so - nobody is charged with judging whether the implementation is correct, the tests test anything, the benchmarks are honest, or the work is complete against its brief; and whatever the orchestrator does notice reaches the implementer as chat, not as a recorded finding the next round can be held to.

DESIGN DECISIONS, with the alternatives rejected and why:
1. NO NEW LIFECYCLE STATES. done -> active is already the single sanctioned backward hop (docs/lifecycle.md:142), with a reopen counter, feedback.max_rounds and escalation to blocked (SPX-SWM-007). The validation phase gates done -> archived and uses the existing reopen as its feedback channel. Rejected alternative: a new validating state between done and archived - it would touch every state-order comparison, every forward-skip rule and every replay path for a distinction the reopen counter already expresses.
2. VERDICTS ARE RECORDED EVENTS BOUND TO CONTENT AND IDENTITY. A review or validation verdict is a journal event carrying the reviewing agent's identity (every journal event already stamps ag) and the hash of what was reviewed (item body for grill; diff for validation). A verdict from the same agent that authored the item (create event's ag) or implemented it (start/submit events' ag) is REFUSED by the server - independence stops being a convention and becomes a computed invariant. A body or diff that changes after the verdict invalidates it by hash mismatch. Rejected alternative: trusting the orchestrator to use different subagents - that is a written-half control; R-0007's organizing finding is that written-half controls are fakeable and this one would be fakeable by simple forgetting.
3. THE SERVER RENDERS AND RECORDS; AGENTS JUDGE. The server computes the packs (grill's critique classes; validation's diff/tests/bench/completeness findings), records verdicts, and gates moves. The judgment itself - reading the pack, deciding, writing findings - is agent work, done by a fresh subagent precisely so its answer is independent of the main context. Rejected alternative: the server scoring quality itself via heuristics - that is how the word-presence checks happened.
4. GRILL MAY DEMAND RESEARCH, NOT PERFORM IT. When grill's computed classes surface unknowns (unanchored targets, zero history/rejection hits for the problem class), the pack emits a research-needed record that counts as an open gap until an R-item exists and is cited. The research itself is a normal R-item driven by the normal flow. Rejected alternative: grill spawning or inlining research - the server cannot run agents, and a tool that blocks on exploration violates SPX-MCP-001's response bound.

WHEN EACH STEP HAPPENS (the placement, part of this elaboration): research (optional, on grill demand) and grill-with-verdict happen between draft and approved - move to=approved gates on a clean independent review verdict. Implementation happens active -> done exactly as today. Validation happens between done and archived - move to=archived gates on a clean independent validation verdict; findings reopen done -> active with the findings as the implementer's next brief, counting a round; max_rounds escalates to blocked as today. Nothing else moves.

CHILD TASKS: one supersedes the earlier grill-verdict draft (same computed classes, now with the verdict event, identity binding and research-demand path); one builds the validation phase (pack, verdict, gate, reopen feedback). The archive-gate task for research consumption (under the backpropagation proposal) is unchanged and composes: an R-item grill demanded must still end consumed or closed.

TOKEN BOUNDS. Verdict events are one journal line each. The validation pack is budget-truncated like grill's. Independence checks are O(item's journal events), already loaded. The expensive mechanisms R-0007 ranked low (mutant kill, oracle ratchet) remain explicitly out of scope until the anti-ceremony lens re-runs against measured costs from these phases.

EXIT CRITERION. On this repository: a draft item receives an independent review verdict from a second agent identity and cannot reach approved before; a done item with a planted vacuous test receives a validation finding, reopens with the finding as its brief, and cannot reach archived until a re-validation verdict is clean.

ROLLBACK. Both gates sit behind config strictness mirroring feedback.grill (require vs warn); removing the config key returns to warn, reverting the commits returns to today. Verdict events in journals are inert history for a reverted server.

## P-01KYD87FJREJ5SD0G2RDCMZ32Y turn review from assertion into evidence: run the gate that exists, then make grill and validation compute what they cannot fake
kind: proposal
state: draft
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
state: draft
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

## T-01KYD87YYZFSJVGX74JG2HD4V3 grill computes its critique and stamps a verdict; the verdict is an independent review event with feedback and a research-demand path; the fakeable word-checks are deleted
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD7QT8YE6PAT515BGPQ5VM4
refs: R-0007, T-01KYD72GCXF998EDDG3BPKZT9W
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
2. VERDICT EVENT. New journal kind EvReview. A new grill op records the verdict: grill op=verdict id=<item> pass=<bool> findings=<text>. The server computes bodyHash = sha256 of the item's body at verdict time and writes it into the event. REFUSALS, each with a dense error: (i) verdict agent ag equals the create event's ag -> "! REVIEW E <id> reviewer is the author - use a fresh agent identity"; (ii) open>0 from the latest pack render and pass=true -> refused, the computed gaps are not the reviewer's to waive; (iii) no pack was rendered for the current body (stored render-hash mismatch) -> refused, the reviewer must grill the current body first. findings text is the feedback: journaled verbatim, rendered by get on the item so the author's next revision sees it.
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

SCOPE: grill.go, the move gate in tools.go, the EvReview constant in journal.go, tests. Do not touch the item model (Grilled stays a string), lifecycle.go's state machine, templates, prompts, or the validation phase (sibling task). tools.go is shared with two sibling tasks - the lease serializes; do not run concurrently.
ROLLBACK: revert the commit. Stamps "<date> open=<n>" stay parseable via the legacy bare-date path you keep; EvReview events in journals are inert history for a reverted server - verify a pre-revert journal still replays post-revert and state it in the report.
REPORT BACK: where each class and refusal is computed, the per-axis scoping conditions, both byte counts, each test's real result including both red-runs, the replay-after-revert check, anything deliberately not done.

## T-01KYD87ZA6F83AKH7THFKBBFZA validate: the post-implementation phase - computed pack over the diff, independent verdict gating archive, findings reopen the item as the implementer's next brief
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD7QT8YE6PAT515BGPQ5VM4
refs: R-0007
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
   #bench - only when the diff touches Benchmark funcs or *_bench_test.go: (a) a Benchmark whose loop does not consume b.N or b.Loop -> "v fakebench <func>" (AST); (b) benchmark numbers claimed in the item's report/notes with no matching Benchmark func in the diff -> "v benchclaim <name>". The validator agent re-runs ONLY the named benchmarks with -benchtime=1x as an execution proof; performance regressions are its judgment, not a server computation.
   #verify - the declared gate/verify commands and their last recorded result from the submit gate journal trail, so the validator sees what was proven versus asserted.
2. GIT AUTOMATION, user-authorized for this set: the server performs actual git commits as phase checkpoints, WITHOUT the driving agent doing anything - the commit is a side effect of the tool call, the agent issues no git commands and needs no git knowledge for it. One workspace config knob git_commits: phases|off, DEFAULT phases (the point is zero agent effort; off exists for repos that refuse tool-made commits). Two uses: (a) DIFF BINDING BY SHA - the validation pack and verdict bind to the git commit range (merge-base of the item's branch to its merge commit, or the commit set citing the item ID), recorded as SHAs in the EvValidate event; a SHA range is content-addressed by git itself, so the stale-verdict check becomes a SHA comparison, no bespoke hashing. (b) PHASE CHECKPOINT COMMITS - when the server writes .spectackle state for a verdict, reopen or gated archive, it commits EXACTLY the .spectackle paths it wrote, message "spectackle: <ev> <item-display-id> [pass|findings=<n>]", so every phase transition is a git-visible, attributable checkpoint. HARD LIMITS: the server never commits files outside .spectackle/ trees, never amends, never pushes - remotes stay with the orchestrator and CI; when the repository has no git or the paths are gitignored, the tool call succeeds and the checkpoint is skipped silently (a checkpoint is a bonus, never a failure mode). Precedent: the submit path already commits and merges via internal/wt; reuse its plumbing, do not shell out anew. ALSO WIRE the same checkpoint behavior into the EvReview verdict path the NEEDS task landed (it merges before this task, so the retrofit lands here): grill op=verdict under git_commits=phases checkpoints its journal write identically. Add a test per phase: verdict, reopen, archive each produce exactly one commit touching only .spectackle paths with the specified message shape; with git_commits=off, zero commits, byte-identical journal behavior; in a workspace without git, no error.
3. VERDICT: validate op=verdict id=<item> pass=<bool> findings=<text>. Journal kind EvValidate. Refusals mirror EvReview exactly: same-agent (verdict ag equals any start/submit ag of the item, OR the create ag when no start exists) -> "! VALIDATE E <id> validator implemented this - use a fresh agent identity"; pass=true while the pack's computed findings > 0 -> refused (computed findings are not waivable; the validator judges ON TOP of them, never instead of them); diffHash mismatch (diff changed since last pack render) -> refused, re-render first.
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

## T-01KYD87ZN7EJ49CMSEQE9XGGWS package-local contract coverage: silent by default with visibility in state, counted by check only under coverage_gate
kind: task
state: draft
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
state: draft
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
state: draft
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
