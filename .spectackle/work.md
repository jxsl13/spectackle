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

## T-0136 tool boundary: accept a short ID prefix everywhere an ID is taken, and name ambiguities
kind: task
state: approved
created: 2026-07-25
parent: P-0088
refs: ADR-0013
targets: internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, internal/mcpserver/decide.go, internal/mcpserver/grill.go, docs/tools.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first.

BLOCKED-ON: the internal/ids and internal/item tasks under P-0088 must both be merged first; verify before starting and stop if either is missing.

WHAT TO BUILD (ADR-0013's display half)
Every tool argument that takes an item ID must accept the shortest unambiguous prefix as well as the full ID: get, move, draft parent and refs, decide item and id, grill, work, lease, knowledge where applicable. Resolve through internal/ids against the set of known IDs, which includes live items AND archived ones reachable as journal tombstones — an ID that only exists as a tombstone must still resolve, or archived history becomes unreferenceable.
Rendering: outputs emit the shortened form, computed against the same known set, so a copied ID from any output is accepted back verbatim. Full IDs must always remain acceptable.
Ambiguity is an error, never a guess: refuse with the existing dense error style, naming every candidate so the caller can disambiguate in one more call. A prefix matching nothing keeps the existing not-found behavior with nearest matches.
docs/tools.md must state the prefix rule once, plainly, since the schema comments are the contract agents read.

TESTS
  full ID and short prefix both resolve to the same record, through more than one tool.
  an ambiguous prefix refuses and names every candidate.
  an unknown prefix answers not-found, not ambiguity.
  a tombstoned archived ID resolves by prefix.
  rendered output uses the short form and feeding it straight back resolves.

VERIFY: go build ./... ; go test ./internal/mcpserver/... -race ; go test ./... ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL, B-0010). Then drive it live over a scratch workspace with the call subcommand: draft, then get by a short prefix, then force an ambiguity and show the error.
SCOPE: the five named files. Do not touch internal/ids or internal/item (merged siblings), and do not start the migration.
ROLLBACK: resolution is additive at the boundary; removing it leaves full IDs working exactly as before.
REPORT BACK: the resolution helper's shape, where you hooked it in, the live transcript, each test's real result, anything deliberately not done.

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

## R-0007 how to make the workflow force multi-perspective analysis, so specifications stop shipping with implementation gaps
kind: research
state: active
created: 2026-07-25
targets: internal/mcpserver/grill.go, docs/agent-workflow.md

PROBLEM
The lifecycle produces well-formed records and still ships implementations with holes. This repository is its own evidence, and the evidence is unusually good because the holes were found later by other means:
- R-0005 probed all 32 supported languages empirically and found major extraction gaps in 30 of them. Every one of those language entries had passed through drafting and review; none had been run against adversarial-but-idiomatic source until that probe.
- Ten defects, B-0001 through B-0010, were found by dogfooding the server on itself rather than by the review step that preceded each change. Four of them hid behind one shared blind spot in the test bed, which no reviewer noticed because every test agreed on the same wrong assumptions.
- A prior rejection already recorded the shape of the error: a hash refresh is not a verification, it only records that something happened. Review artifacts can be produced without the underlying property being checked.

WHAT EXISTS TODAY
grill renders targets, contracts, child-brief heuristics, tests and rejections, then asks a fixed checklist. Its brief heuristics are substring and length checks (body length, a slash for a path, the literal text of a verify command), and its questions fire when a word is absent from the body. All of them are satisfiable by writing the word, which is the anti-ceremony problem in miniature. research aggregates read-only context. decide records choices. check compares anchors. None of these demands that a claim be demonstrated.

QUESTION
What mechanisms would make the workflow structurally demand that a problem be examined from several independent angles, and that implementation claims be demonstrated rather than asserted, without degrading into ceremony that inflates every task body?

METHOD: six independent lenses, run in parallel, each answering from evidence rather than from opinion, then synthesized.

EXIT CRITERION: a ranked set of concrete mechanism proposals, each with the failure it would have caught in this repository's own history, its token and latency cost, and its ceremony risk with a mitigation. Anything that cannot name a real failure it would have caught is to be reported as such and dropped.
