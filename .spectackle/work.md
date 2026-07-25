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
