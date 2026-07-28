---
schema: v1
---

## ADR-01KYJMWE1NFJ7VZ82GX3YK0FMZ Benchmark frames: os/arch/cpu/ram/gpu are required keys. May a machine-independent benchmark (byte counts, token curves) use the sentinel any (dimension irrelevant) so one key spans hosts, or must every benchmark pin real host values?
kind: adr
state: done
created: 2026-07-27
decision: allow the any sentinel for machine-independent dims
consequences: Machine-independent benchmarks (byte counts, token curves) share one unique key across hosts via any; none stays for genuinely absent hardware; host-dependent benchmarks still pin all five real values. The key canonicalization treats any as a first-class value, and cmp across frames renders the sentinel verbatim.
status: accepted

kind: radio
option: allow the any sentinel for machine-independent dims
option: always pin real host values - no sentinel
blocks: P-01KYJMVX2QES89YTP3KXSJPA7J
choice: allow the any sentinel for machine-independent dims

## ADR-01KYJMWEWQE48T3PR76TYQRD3H Benchmark history at default depth 1: when a new version supersedes the old, what survives? The put-time delta summary (better/worse/tie per metric) is always journaled; should the superseded RAW metric values also ride the journal event (bounded per-put growth, richer regression forensics), or is the summary enough?
kind: adr
state: done
created: 2026-07-27
decision: raw values ride the journal event too
consequences: USER CHOSE the richer option over the lean recommendation: every put that supersedes a version appends the outgoing versions full metric values to the journaled delta event - bounded per-put growth, full regression forensics at depth 1. The put event schema carries prior impl/metric values alongside the better/worse/tie summary; compaction keeps the event class.
status: accepted

kind: radio
option: summary only - raw superseded values are destroyed
option: raw values ride the journal event too
blocks: P-01KYJMVX2QES89YTP3KXSJPA7J
choice: raw values ride the journal event too

## ADR-01KYKTGGPREG2B7XJ1FTY25E7S Worktree contention: enforce the lease at work op=start, or keep merge-layer arbitration?
kind: adr
state: done
created: 2026-07-28
context: The swarm-contention benchmark (M-01KYKSKKPDFNT, B-01KYKSKMHNE2H) proved work op=start creates NO file-target lease despite SPX-SWM-003 documenting an auto-claim: two concurrent agents on the same declared target both start, both implement, and the slower one pays a full implement-then-resolve round at submit (measured ~20 calls wasted vs 1 refused call). Convergence is safe either way - zero lost updates. The choice is the coordination contract.
decision: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)
status: accepted

kind: radio
option: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)
option: warn: start renders the l-line naming the holder but proceeds - informed parallelism, the second agent chooses
option: redocument: leases stay advisory for the worktree flow; SPX-SWM-003 and work docs updated to name the merge layer as arbiter - never blocks
blocks: B-01KYKSKMHNE2HS9H235BG6DV4B
choice: enforce: start claims normalized targets, live foreign overlap refuses with the l-line naming the holder - token-minimal, matches the docs as written (recommended)

## ADR-01KYMKEG7YE2PS8DSJZJW799P9 knowledge merge reports conflicts but no op can resolve them — which shape should resolution take?
kind: adr
state: done
created: 2026-07-28
context: The gap hunt proved (P-01KYMCKE8DEW7) that internal/knowledge implements Resolve/Apply so a human can pick a winning decision and carry it forward with the loser preserved, but no MCP op reaches it: knowledge accepts export|merge|apply only. merge honestly reports conflicting ADRs as x lines and EXCLUDES them from the condensate, so applying that condensate lands NEITHER side and the only way to carry a curated outcome forward is hand-editing the artifact markdown - defeating the server-is-the-only-writer model.
decision: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build
status: accepted

kind: radio
option: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build
option: knowledge op=resolve key=<conflict key> choose=<source> - a direct op writing the winner plus a resolution block into the condensate; smallest new surface, but a second decision channel beside decide
option: document-only: state that conflicts are deliberately excluded and curation happens outside the tool; zero code, but the promise that curation is a humans call keeps having no call
blocks: P-01KYMCKE8DEW7BZ3FNCMJTNSG2
choice: decide-integration: each conflict mints an ADR in the applying workspace and answering it selects the winner - reuses ASK-SURFACE-001 and the existing decide UI, no new grammar, heaviest to build

## B-01KYN3E973F20VH7DHPE1YSSD7 a newline in an ADR header field silently swallows every field after it into the body
kind: bug
state: draft
created: 2026-07-28
targets: internal/item

internal/item/item.go LoadWork parses the machine header as a run of contiguous key: value lines and breaks at the first line without a ": " separator. A field VALUE containing a newline therefore ends the header early: its continuation line has no separator, the loop breaks, and every header field written after it becomes part of Body instead of a struct field. Silent - no error, no warning.

REPRODUCTION (found by an independent validator during T-01KYMPN0PNEWV, confirmed pre-existing: git diff origin/main...HEAD -- internal/item/ is empty). knowledge export entries=[{kind: adr, context: "Line one.\nLine two.", decision: go-with-A, status: accepted, options: [...]}] then knowledge apply. get shows decision and status swallowed into the body text; the reloaded items .Decision and .Status are empty strings.

IMPACT is not cosmetic. Every consumer that reads those fields sees them as unset on a record that plainly has them: the archive tombstone retains an empty decision (so an archived multi-paragraph ADR loses which option won - the same loss class LC-001 was written to close, arriving through a different door), knowledge.Extract exports an ADR with no decision, and knowledge apply then reports a spurious divergence between two repositories that actually agree - the validator observed x adr ... ours="" theirs="keep as is" for identical content, caused purely by the local copy being corrupted on reload. Multi-paragraph context and consequences are the NORMAL shape for a real ADR, so this is reachable by ordinary use, not by adversarial input.

DIRECTION, not a decision - the fix needs the design context behind the work.md format. Either the header parser learns continuation lines (indented, or explicitly terminated), or the writer escapes newlines on the way out and unescapes on the way in, or the writer refuses a value it cannot round-trip rather than writing one that silently truncates. Whichever is chosen, the round trip needs a property test over values containing newlines, leading/trailing whitespace and separator characters - the existing tests only exercise single-line values, which is why this survived.

VERIFY: a test that writes every ADR field with an embedded newline, reloads, and asserts field-for-field equality.

## P-01KYN5YCXGENMRNK00CQTPJM1P the leave-work.md boundary is lossy: seven fields are dropped or corrupted when a record archives or is rejected
kind: proposal
state: draft
created: 2026-07-28
targets: internal/lifecycle, internal/item, internal/journal, internal/replay

An independent gap hunt, run after LC-001 was written, probed for more instances of the class that rule records: a record's substance compressed at a lifecycle boundary, destroying the only copy. It found seven, all at the same boundary - the moment a record LEAVES work.md - and all confirmed empirically in throwaway repos by planting a marker string and grepping the whole .spectackle tree for it afterward. item.LoadWork/writeWork round-trip every field faithfully while a record stays in work.md, so nothing here is a parse bug; the loss is entirely in what the journal event carries.

G1 REJECT drops an ADR's Context, Decision, Consequences and Status entirely. Both EvReject construction sites build from Body/Tg/Par/Rls/Rnd/Gr/Nd/Ov only. archive() captures exactly these four fields via adrOutcome (that was the LC-001 fix); reject never got the same treatment. Reject then revoke a decided ADR and the fields are gone from work.md, from get and from the raw journal - unrecoverable. Reachable by the single most obvious revisit-a-decision workflow the ADR feature has.

G2 REJECT drops Refs. journal.Event.Refs is documented \"archive/reject: item refs\" and archive does set it; neither reject site does. A two-line omission against the field's own contract.

G3 REJECT then revoke CORRUPTS Created. No Created channel exists on any event, so lastReject leaves it empty and item.Upsert's default-to-now stamps a fresh, wrong date over the real one - silently, and indistinguishably from a real value. Corruption is worse than absence: archive leaves it blank, which at least reads as unknown.

G4 ARCHIVE drops Parent and Targets. EvArchive has no Par/Tg fields at all, though EvReject does. So the FAILURE path preserves structural data the SUCCESS path discards - the same inversion the two already-fixed bugs had.

G5 ARCHIVE drops Rounds, Grilled, Needs and Override. Partially recoverable via EvEscalate/EvDecide, but only for items that escalated. An item that reopened once or twice below the escalate threshold loses Rounds at the next compaction; Grilled is worse - EvGrill is in compaction's unconditional fold bucket and archive never captures it, so a grill verdict is gone the instant its item archives.

G6 WORKTREE SUBMIT truncates the spec.md intent line. replay.intentLine is \"- \" + ID + \" \" + Title, with no note and no gist, while lifecycle.archive appends \": \" + firstOf(note, gistLine). Git never merges .spectackle text and replay.Run is main's only writer, so EVERY item archived through the swarm/worktree flow - the documented primary workflow - lands on main with a stripped intent line. The note survives in the replayed journal event, so this is degradation rather than loss, but the one artifact meant to be the permanent human-readable trace is the one that is incomplete.

G7 Goal and Rules have no write path at all. Nothing assigns either outside item.go's own parser; draft and the draft-revise handler touch only Title/Body/Targets/Refs. Goal is READ by three gate paths (gitflow, swarm, validate), so a documented gate is unreachable except by hand-editing work.md, which the server's own instructions forbid.

SHAPE. Not seven patches. The root cause is that journal.Event's field set was grown per-need and now disagrees with item.Item's in both directions, with no test asserting the correspondence. The work should decide, once and explicitly, what a tombstone owes each kind, then make the correspondence mechanical - a round-trip property test over every Item field through every boundary that serializes one (archive, reject, escalate, compact fold, worktree replay), so the next field added to Item cannot silently fall through. G3 additionally needs a decision on whether Created belongs in the event or should be derived from the record ID's own UUIDv7 timestamp, which ids.ParseRecordID can already read.

RELATED, filed separately: B-01KYN3E973F20 (a newline in a header field makes LoadWork swallow every later field into Body) is the same data-integrity area but a parser bug, before the boundary rather than at it.

ALSO FOUND, off-class, flagged for triage not for this proposal: a git-flow-gate-failed archive that is compensated back to done does not restore the child items the same call already folded away, and does not roll back its spec.AppendIntent - leaving a permanent duplicate intent line and a child reachable only as a tombstone. Transactional-boundary bugs, not compression bugs.
