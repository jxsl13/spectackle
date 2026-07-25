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

## P-01KYD6VP6VE2Z8A517AT3RP39T backpropagation: every loop result flows back into the workspace, and the server names the next step so no step can be silently skipped
kind: proposal
state: draft
created: 2026-07-25
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, internal/mcpserver/tools.go, docs/agent-workflow.md

PROBLEM. The loop's forward path is well defined: research, draft, grill, approve, implement, check, archive. The backward path - how results change the workspace so the next iteration is smarter - exists only as convention in the orchestrator's head. Three symptoms, each verified in this repository: (1) the server's own backprop concept covers exactly one flow, code-to-spec drift (check fix=true drafts one proposal per drifted rule, tools.go:1733) - research results, implementation reports and rejections have no defined return path; (2) the workflow template's final step says archive and commit, but not what must be captured (the archive note is the training signal - it becomes the journal tombstone and the FTS body future sessions search - yet nothing says so and an empty note passes); (3) the template omits the post-merge restart entirely: CONTRIBUTING.md mandates make dev after every merge because the resident server IS the product under change, and the machine-facing instructions never mention it, which produced real stale-binary confusion this session.

WHY IT MATTERS FOR TOKEN COST. Knowledge that does not land in one of the three durable stores (spec.md rules, journal tombstones with substantive notes, knowledge artifacts) is re-derived by a later session at full exploration price. Every re-derivation this repository has already paid - the T-0094-vs-T-0138 migration re-litigation was avoided only because the rejection note happened to be thorough - is the cost of an undefined backward path. The backward path is the token-saving mechanism, not an overhead on it.

DELTA. Two child tasks:
1. Define the loop's backward edges in every machine-facing surface (server instructions, workflow template, next-step prompt) so each state names its one next action and each completed item states where its learning landed. Bounded: hints are one line, computed from actual state, no new prose sections.
2. Enforce the research return path at the one gate that can see it: archiving an R-item requires either a consumer (a live item or rule citing it) or an explicit no-action note. One conditional at one call site; no sweeps, no background scans.

EXPLICITLY REJECTED, to bound scope: a generic workflow engine that re-orders steps; any always-on background process; LLM-written self-assessments as evidence (they are the written half R-0007 showed is fakeable - every new signal here is either server-computed or a hard gate).

EXIT CRITERION. A fresh orchestrator session driven only by the server's own prompts (workflow, next, state) performs research capture, archive notes, and post-merge restart without any of them being in its own system prompt - measured by driving the loop once headlessly and checking the three stores gained the expected records.

ROLLBACK. Each surface change is a template/instruction edit; the R-item gate is one conditional. Reverting the commit restores the prior loop; no data format changes.

SCOPE DISJOINTNESS. Task 1 touches server.go/prompts.go/templates/docs; task 2 touches the move path in tools.go. T-01KYD5R (grill verdict) also touches tools.go's move path - task 2 declares NEEDS on it and runs after it merges.

## T-01KYD72GQ6E2ZV0HX8S443NPY6 package-local contract coverage: the gap branch becomes reachable, reported bounded, and gated only by explicit config
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD47GZ7FAMAGM4NEF0BQS8T
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/tools.go, internal/workspace/workspace.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

WHY. check cannot report a contract gap in this repository: the root bundle is unscoped and carries 16 rules, so spec.Cascade.ForPath never returns empty and the coverage branch (tools.go:1695-1716, fires on len(ForPath(rel))==0) is structurally unreachable. Twelve of twenty-four packages under internal/ carry no bundle while SPX-REPO-002 mandates one, and check answers ok. Six of the ten dogfooded defects landed in exactly those uncovered packages. R-0007 ranks this as a verified failure class; the known weakness is that any EARS sentence silences it, so the mitigation below is part of the definition, not an option.

VERIFIED GROUND (do not re-derive)
- coverageGaps at tools.go:1695: walks source dirs, emits for len(c.ForPath(rel))==0. ForPath returns the full cascade for a path, root rules included - that inclusion is what makes the branch dead.
- check's final ok is decided by the findings tally; g orphan records (MCP-004) count toward it. The CI self-hosting gate requires check's output to end exactly ok - ANY new record class that counts immediately turns this repository's own CI red with 12 findings. That is the central design constraint of this task.
- Workspace config (internal/workspace/workspace.go, Config struct ~line 42) already parses feedback and compact blocks; adding a key is mechanical.

WHAT TO BUILD
1. COVERED(pkg) definition: a source dir under internal/ or cmd/ is covered iff (a) a non-root bundle exists at it or at an ancestor below the root, or (b) at least one root-bundle rule binds a node inside it via applies (the anchors table resolves applies targets to paths - use it; a rule whose applies is empty never covers anything outside its own dir). This is the mitigation: a lazily written root-level EARS sentence with no applies binding silences nothing.
2. REPORTING: one line per uncovered dir, "g nocontract <dir>", sorted, capped at 20 lines with a "+<n> more" tail. Emitted by check in the same section as today's coverage gaps.
3. GATING: by default the nocontract class is reported but EXCLUDED from the findings tally, so existing repositories (this one included) stay ok and CI stays green. A new workspace config key coverage_gate: package flips it into the tally. Default absent = today's behavior plus visibility. This repository does NOT set the key in this task - backfilling 12 packages' contracts is follow-up work the report lists, not work this task does.
4. The 12 currently uncovered dirs are the acceptance fixture: the report pastes check's real output on this repository showing exactly which dirs it names.

NON-NEGOTIABLE PROPERTIES, each with a test
- A repo whose root bundle holds N rules with no applies bindings reports every internal/ package as uncovered; adding one rule with an applies binding into pkg X removes exactly X from the list.
- With coverage_gate absent, a workspace with uncovered dirs still ends check with ok; with coverage_gate: package it does not, and the findings count includes them.
- Output is bounded: a synthetic workspace with 40 uncovered dirs emits 20 lines + the tail line, never 40.
- No behavior change to the existing orphan/drift/duplicate classes (their tests keep passing untouched).

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' - must still end ok on this repository AND list the nocontract records; paste the full section in the report.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs check on the worktree and on a synthetic gated workspace from the diff alone; verdict recorded in the archive note.

SCOPE: coverageGaps and its call site in tools.go, the config key in workspace.go, tests. Do not touch grill.go (T-01KYD5R... owns it), the spec package, or the anchors format. tools.go ordering: run after the grill-verdict task merges; the lease enforces non-concurrency.
ROLLBACK: revert the commit. The config key is additive; a workspace that set coverage_gate keeps working (unknown keys are ignored by the YAML parser - verify and state this in the report).
REPORT BACK: the covered() definition as implemented, the real check output on this repo, each test's result, the follow-up list of the 12 dirs, anything deliberately not done.

## T-01KYD72H15EPV8KCW6ASSMEFZX evidence sweeps scoped to an item's targets: declared-but-unconsumed symbols and minority call shapes, as a package grill renders
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD47GZ7FAMAGM4NEF0BQS8T
refs: R-0007
grilled: 2026-07-25
targets: internal/evidence, internal/mcpserver/grill.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

NEEDS: the grill-verdict task (grill computes its critique and stamps a verdict) must be MERGED first - this task adds sections to the pack that task restructures, and both touch grill.go. Do not start while it is open.

WHY. Two defect classes from this repository's history are visible statically at review time, scoped to an item's targets, and neither is anything an author can write around:
- B-0009: a schema column was declared and never written or read - the declared-but-unconsumed shape. The title of that bug is literally the finding.
- B-0003: workAbort passed an item ID where twenty sibling call sites passed a directory - one caller diverging from the shape every other caller agrees on. One against twenty is a signal no word-check can see.
Both sweeps run only over an item's declared targets, which is what keeps them cheap and their output bounded - a global sweep is explicitly out of scope and was considered and rejected for unbounded output.

VERIFIED GROUND (do not re-derive)
- The graph (internal/graph) stores nodes and edges; EdgeKind covers EDef/ECall/EUse and friends, but edges carry NO argument metadata - graph.go's Edge has no arg fields. The caller-divergence sweep therefore CANNOT come from stored edges alone: it re-parses the call sites' files (go/ast for Go) for the callee's name, bounded to the files the graph's inbound ECall edges name. The graph gives you the file list; the AST gives you the shapes. For non-Go targets, skip - state so in output as "e skipped <node> non-go".
- The unconsumed sweep CAN come from the stored graph: exported symbols (nodes) under a target path with zero inbound ECall/EUse edges from outside their own file. Vendored/test-only consumers count as consumers; init-time registration counts (EUse). The known false positive class is reflection/plugin lookup - cap the sweep's claim accordingly: the record reads "e unconsumed <node> no inbound edges", a lead for the reviewer, never an error.
- grill's pack sections and budget truncation: grill.go (post-restructure by the needed task). New sections render before #rejections, after the computed classes.

WHAT TO BUILD
1. Package internal/evidence with two pure functions taking the graph + target paths (+ file source access for the AST pass): Unconsumed(g, targets) []Record and DivergentCallers(g, targets, load func(path) []byte) []Record. Deterministic order, each Record renders to one line, both capped at 10 records with a "+<n> more" tail.
2. Divergence definition, precise: for each callee node under targets with >= 5 inbound call edges, group call sites by (argument count, per-argument shape class) where shape class is one of literal-kind (string/int/composite), identifier, call-result, selector - computed from the AST. Report groups whose share is <= 20% as "e divergent <callee> <k>/<n> sites differ: <file:line>..." (first 3 sites). Thresholds are consts in evidence with a one-line rationale each; B-0003's 1-of-21 must trip, and a 50/50 split must not.
3. Wire both into grill as an #evidence section, subject to the pack budget, counted into the verdict's open-gap tally ONLY for the unconsumed class when the item's kind is task or bug (a proposal legitimately names targets it will not consume yet); divergent records are always informational (they may be the point of the change).
4. Cost ceiling, enforced not aspirational: the AST pass parses only files the graph names as call sites of target callees, never the whole tree; a guard refuses more than 50 files and reports "e truncated ast >50 files" instead. SPX-MCP-001 (1 MiB reads, 2s warm response) applies - add the evidence pass to whatever timing test asserts it, and report the measured wall time on this repository with targets internal/mcpserver (the largest package).

NON-NEGOTIABLE PROPERTIES, each with a test
- A fixture reproducing B-0009's shape (exported symbol, zero inbound) is reported; adding one consumer removes it.
- A fixture reproducing B-0003's shape (21 call sites, 1 divergent) reports exactly the divergent site; a 10/10 split reports nothing.
- Caps hold: fixtures with 30 unconsumed symbols / 30 divergent callees emit 10+tail each.
- Determinism: two runs on the same fixture emit byte-identical sections.
- Non-Go targets skip cleanly with the skipped record, no error.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Run grill on a real item in the worktree with targets internal/mcpserver and paste the #evidence section plus its wall time in the report.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the two fixture tests and the real-item grill from the diff alone; verdict recorded in the archive note.

SCOPE: the new internal/evidence package, the grill wiring, tests. Do not modify internal/graph (no schema change for arg metadata - that was considered and rejected: it grows every edge for one consumer), the index, or the item model.
ROLLBACK: remove the #evidence section call; the evidence package is dead code until deleted. No stored state.
REPORT BACK: measured wall time and output bytes on the internal/mcpserver run, both fixtures' real outputs, threshold values as landed, each test's result, anything deliberately not done.

## T-01KYD72HB0FHX9G80DQGS9YBB1 the backward path in every machine-facing surface: state-computed next steps, archive notes as the training signal, post-merge restart in the loop
kind: task
state: draft
created: 2026-07-25
parent: P-01KYD6VP6VE2Z8A517AT3RP39T
refs: R-0007
grilled: 2026-07-25
targets: internal/mcpserver/server.go, internal/mcpserver/prompts.go, internal/mcpserver/templates/commands/workflow.md.tmpl, docs/agent-workflow.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. This task edits instructions, templates and one prompt function - no lifecycle semantics change.

WHY. The loop's backward path - results changing the workspace so the next iteration is smarter - is convention, not definition. An LLM driving the loop from the server's own surfaces is never told: that the archive note becomes the journal tombstone and the FTS body every later session searches (the note IS the training signal); that research results must land as rules, ADRs, tasks or an explicit no-action note; that after every merge the resident server must be rebuilt (CONTRIBUTING.md mandates make dev; the machine-facing surfaces never mention it - real stale-binary confusion resulted this session); or what the single next action is for the state an item is actually in. Each omission costs a later session full re-derivation price, which is the opposite of this project's token-economy goal.

VERIFIED GROUND (do not re-derive)
- server.go instruction manifest: ORCHESTRATION and TOKEN ECONOMY paragraphs exist (~line 44); SPX-MCP-006 pins the TOKEN ECONOMY paragraph's presence. There is no BACKPROP paragraph.
- templates/commands/workflow.md.tmpl: 8 steps; step 7 check, step 8 archive+commit. No make dev, no note guidance, no research-capture step. templates/commands/ holds 8 tmpl files; the commands tool regenerates .claude command files from them - regeneration is part of this task's verification.
- prompts.go: promptNext (line 158) picks an actionable item and renders its brief; promptWorkflow (line 89) renders lifecycle steps; lifecycleLines (line 68). promptNext already skips blocked items and open needs.
- check fix=true already drafts one backprop proposal per drifted rule (tools.go:1733) - the code-to-spec direction exists; do NOT duplicate it in prose, reference it.

WHAT TO BUILD
1. server.go: one BACKPROP paragraph in the instruction manifest, <= 700 bytes, stating the three durable stores (rules in spec.md, tombstone notes in the journal, knowledge artifacts), that every completed or rejected item must leave its learning in one of them, that the archive/reject note is searched by future sessions (write substance, not ceremony), and that after every merge the resident binary is rebuilt (make dev) because the server is the product under change. Add a matching EARS rule via the normal rule path binding it, mirroring how SPX-MCP-006 pins TOKEN ECONOMY.
2. workflow.md.tmpl: extend to the closed loop. Step 7 gains: the declared verify commands run through the gate, and an INDEPENDENT verifier (fresh context) re-runs the VERIFY block from the diff alone before archive - the implementer's own transcript is never the evidence. Step 8 gains: the archive note requirement (what changed, what was measured, what was deliberately not done) and where the learning landed (rule/ADR/rejection/note). New step 9: make dev after merge, with one sentence why. New step 10: research capture - every R-item ends as consumed (cited by rules/items) or explicitly closed no-action; cite that the server enforces this at the archive gate (the sibling task). Keep the template's total growth <= 40 lines - this is a checklist, not an essay.
3. promptNext: for each item state, the rendered output's first line is the one next action, computed: draft->grill (if ungrilled) or close gaps/submit; approved->work op=start; active->work op=submit when the worktree is clean, else implement; done->check then move to=archived with note; blocked->decide op=answer on the linked ADR. One line each, no new sections. States it already handles keep their behavior; add only what is missing.
4. docs/agent-workflow.md: a short BACKWARD PATH section (human-facing mirror of 1-3), and the independent-verification sentence in the orchestrator role description.

NON-NEGOTIABLE PROPERTIES, each with a test
- Instruction manifest growth is bounded: a test asserts the BACKPROP paragraph exists and the whole manifest stays under its current size + 800 bytes (measure current size first, hardcode the ceiling with a comment naming the measured base).
- commands op=gen regenerates the command files and the generated /spectackle command contains steps 9 and 10 - test on a temp workspace.
- promptNext on a fixture item in each state (draft ungrilled, approved, active, done, blocked) opens with the exact expected action line - table test.
- No lifecycle behavior changes: the full existing test suite passes untouched.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  spectackle call -root <worktree-root> commands '{"op":"gen"}' (or the harness-detection path) succeeds; paste the regenerated workflow command's new steps in the report.
CROSS-VERIFICATION (orchestrator, after done): independent verifier regenerates the commands file and diffs it against the report's claim; verdict in the archive note.

SCOPE: the four named files plus generated command files and tests. Do not touch tools.go (two sibling tasks own its regions), lifecycle.go, or grill.go. No new tools, no config keys.
ROLLBACK: revert the commit; regenerate commands once (op=gen) to restore the previous command files. No stored state.
REPORT BACK: manifest base and final byte sizes, the regenerated steps verbatim, each test's result, anything deliberately not done.
