---
schema: v1
---

## B-01KYD1G9RAEHWTK3SW3ZH3YFWS the stale-binary hint fires on released and packaged binaries, where its advice cannot be followed
kind: bug
state: draft
created: 2026-07-25
targets: internal/mcpserver/swarm.go

GitHub issue 29. This is a defect in the MCP-010 hint shipped by T-01KYB2318RFFGV6NA9WBWABMYB, found by field use of the released binary rather than by the development checkout it was built and tested in.

OBSERVED: every tool call prepends the hint naming make dev, including on a freshly installed release binary in a repository that contains no Makefile at all. The advice is unfollowable for anyone who installed spectackle rather than building it. It fires on every tool without exception.

WHY IT IS WORTH FIXING RATHER THAN TOLERATING, per the reporter: it sits on the token path of a server whose stated purpose is long-term token efficiency, costing a fixed tax on every result while carrying no information for the majority of users; and it trains callers to filter h lines wholesale, which is the same record class used for real signal such as commands op=detect reporting a detected harness. A noisy channel gets filtered, and the useful records go with it.

ROOT CAUSE IN OUR OWN TERMS: the check compares the executable's modification time against the newest source file under the workspace root. In a development checkout that is meaningful. For an installed binary the sources under the user's own repository are almost always newer, so the condition is permanently true and says nothing. The feature was verified only from the perspective it was written in.

FIX DIRECTION: fire only where the advice is actionable — a development checkout of spectackle itself, where a rebuild is both possible and relevant. The staleness check already stats the executable, so it has enough information to recognize that it is not running from a development build; consider also that the version stamp distinguishes a tagged release from a dev build.

VERIFY: an installed binary in an unrelated repository emits no hint on any tool; a development checkout with sources newer than the binary still emits exactly one per crossing; the existing debounce and once-per-crossing tests keep passing.

## T-01KYD88M80EQEAJDW0AB243ZK2 research return path enforced at the archive gate: an R-item archives only consumed or explicitly closed
kind: task
state: submitted
created: 2026-07-25
parent: P-01KYD87FX0F6YRX49R3A8TB6E4
refs: R-0007, T-01KYD72HNHEYAB0WF42BTR31CW
grilled: 2026-07-25
targets: internal/mcpserver/tools.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Supersedes the rejected draft in refs; its validation round found the cost-flatness claim was self-report-only and the co-dependency was named by paraphrase - both corrected; everything else re-recorded intact.

NEEDS: the grill-verdict task (title: grill computes its critique and stamps a verdict) must be MERGED first - it restructures the move path in tools.go this task adds one gate to. The package-coverage task (title: package-local contract coverage: silent by default with visibility in state, counted by check only under coverage_gate) also touches tools.go; whichever merges first, rebase on it - the regions are disjoint (move gate vs check path).

WHY. Research that changes nothing is pure token cost, and nothing today notices. R-0007 is the near-miss proving the class: its findings survive ONLY because the orchestrator chose to write the follow-up proposal - had the session ended first, six lenses and 63 mechanisms would have archived into a tombstone nothing cites, and the next session would re-pay full price. This gate is the smallest mechanism making the return path mandatory: one conditional at one call site, no sweeps, no background work.

VERIFIED GROUND (do not re-derive)
- The move path in tools.go (post grill-verdict restructure) validates to= transitions; the grill gate pattern there is the shape to mirror.
- Items carry Refs (draftIn.Refs, tools.go:60; item.Refs, item.go:69); rules carry rationale text; consumer lookup = live items' Refs (item.LoadAll, already loaded) + archived tombstones (lifecycle.Tombstone, lifecycle.go:507, confirmed exported) + rules whose rationale names the R-id (cascade in memory).
- LCY-001 binds tombstone resolution; an archived consumer counts.

WHAT TO BUILD
1. At move to=archived (and any shortcut implying it) for kind=research: require at least one consumer - a live or archived item whose Refs include the R-id, or a rule whose rationale names it - OR a note of at least 80 characters explicitly closing it. Refusal: "! BACKPROP E <id> unconsumed research - cite it from a rule/item or close with a no-action note".
2. LAYERING, stated in a code comment: the 80-char floor is a TRIPWIRE against accidental emptiness, gameable by padding and known to be (this set's own validation said so); the floor's job is stopping the silent case, substance is the consumer path and human review. Do not present the floor as substance verification.
3. The refusal is hard regardless of feedback config - an unconsumed-and-unexplained archive has no legitimate loose mode; comment states this asymmetry versus the grill/validate knobs.
4. Reject stays untouched: a rejected R-item is a recorded dead end, which IS a return path.
5. Cost flatness, COMPUTED not self-reported (corrected): a test loads the workspace, then makes the .spectackle tree unreadable (rename the directory out from under the loaded Root, or chmod 0o000 on POSIX - pick the portable one for CI, justify), then exercises the gate on the loaded state: it must answer correctly with zero filesystem reads, proven by the tree's absence. The diff-review sentence from the prior draft remains as belt, this test is the suspenders.

NON-NEGOTIABLE PROPERTIES, each with a test
- Zero consumers, no note -> exact refusal; same item, 80+ char note -> archives; same item cited by one task's Refs -> archives without note.
- An archived consumer counts (archive the consumer first, then the R-item).
- A rule whose rationale cites the R-id counts (through rule op=add).
- Non-research kinds untouched (existing tests unmodified).
- The no-read test from point 5.

VERIFY (real output, never predicted)
  go build ./... ; go test ./... -race ; go vet ./... ; gofmt -l . (empty)
  spectackle lint <worktree-root> (positional)
  spectackle call -root <worktree-root> check '{}' ends exactly ok
  Red-run: the refusal test written first, shown failing against current code; paste the failing output.
CROSS-VERIFICATION (orchestrator, after done): independent verifier re-runs the five fixtures including the no-read test from the diff alone; verdict recorded in the archive note.

SCOPE: the move gate region of tools.go plus tests. Do not touch grill.go, lifecycle.go's state machine, the item model, templates.
ROLLBACK: revert the commit - one conditional, no stored state, no format change.
REPORT BACK: where the gate landed, the consumer lookup, the no-read test's mechanism and result, each fixture's real result including the red-run, anything deliberately not done.

## B-01KYDDR98HEXE80DJ3JJCY9A8M the draft pull request cannot be opened on entry to active: the branch has no commits yet
kind: bug
state: done
created: 2026-07-25
targets: internal/mcpserver/gitflow.go

DEFECT, found by the automation running against a real forge on its first live transition after merge.

OBSERVED: move to=active on a task emitted the branch record and then
  ! GIT W <id> pr open: 422 Validation Failed - No commits between main and spectackle/<id>
The branch was created and pushed correctly; only the pull request failed.

CAUSE: the transition mapping opens the draft pull request at the moment the item enters active, but at that instant the task branch is identical to base — the work has not happened yet, so CommitCode has nothing to commit. GitHub refuses to open a pull request with no commits between head and base, and it is right to: an empty pull request describes nothing.

WHY THE OFFLINE TEST DID NOT CATCH IT: the offline forge tracks pull requests itself and has no such precondition, so the same sequence succeeds locally. The live run against GitHub is what exposed it. That asymmetry is worth keeping in mind for every future step of this mapping — the offline implementation is a lifecycle double, not a fidelity double.

WHY IT MATTERS: it is the first step of the workflow, so the whole always-covered invariant fails at the point it is supposed to start. The branch exists and is pushed, which is the state the policy explicitly forbids: a pushed branch with no pull request is work nobody can find from the review surface.

FIX DIRECTION: the pull request is opened as soon as the branch HAS a commit, not at a fixed transition. On entry to active, open it only when the branch is already ahead of base; otherwise skip silently and let the next sync — the checkpoint push that happens while the task is active, and the flip on done — open it. The requirement is that a draft exists while work is ongoing, which is satisfied by opening at the first commit rather than at the first transition. Do not seed an empty commit to force the pull request open: it pollutes the per-edge trail the never-squash policy exists to protect.

VERIFY: against a real remote, a task entering active with no changes yet emits the branch record and NO pull-request warning; after the first code change the draft pull request exists; and the flip on done finds it. Offline keeps behaving as it does today.

## T-01KYDEJ4QHEZR9BKYQ16SJSGBA a task's referenced GitHub issues close when its work merges
kind: task
state: done
created: 2026-07-25
refs: ADR-01KYDBQGMRFBN9SAHCWWNSAKX4
targets: internal/mcpserver/gitflow.go

Requirement: an issue referenced by a task closes automatically when that task completes.

MECHANISM CHOSEN, and why it is not an API call. The pull request body carries `Closes #N` for every issue the item references, and the forge closes them when the branch merges. That is idiomatic, atomic with the merge, and it cross-links the issue to the pull request in both directions for free. The rejected alternative was closing each issue through the API at the archive transition: it duplicates a mechanism the forge already has, and it can close an issue for work that never shipped, because archive is a record-state transition and says nothing about whether the branch landed. Tying the close to the MERGE means an issue closes exactly when the fix reaches the base branch, which is the property the requirement actually wants.

PARSING, the part with real blast radius. Closing an issue is public and outward-facing, so a false positive closes a stranger's issue. Two constraints follow. Match only the explicit prose form this repository already uses — GitHub issue N, and the list form GitHub issues N and M / N, M — case-insensitive, since that is what every existing bug body writes. Do NOT match a bare #N: in these records a bare number is far more often a rule ID, a record count, or a pull request, and the cost of being wrong is asymmetric. An item that references nothing produces no Closes line and no behavior change at all.

SCOPE OF THE REFERENCE: the item's title and body, since both carry them in practice (the four field-reported bugs put the reference in the body's first line; nothing prevents a title).

DEDUPLICATION: an item naming the same issue twice, or naming issues 25 and 30 in two separate sentences, produces each number once, in ascending order, so the pull request body is stable rather than dependent on prose order.

WHAT TO BUILD
1. A parser over the item's title and body returning the referenced issue numbers, deduped and sorted.
2. gitPRBody appends one `Closes #N` line per reference, after the brief, so a reader sees the task first and the linkage last.
3. Nothing else changes: no new transition, no API call, no config.

TESTS
  singular and plural prose forms, mixed case, and the two list shapes (N and M; N, M, and K).
  a bare #N is NOT matched, asserted explicitly — this is the false-positive guard and the whole reason the parser is conservative.
  a number that is part of another token (a rule ID, a record ID, a version) is not matched.
  duplicates collapse and output is ascending.
  an item with no references yields a body byte-identical to today's, so the common case is provably unchanged.
  the composed body contains one Closes line per issue and the brief above it.

VERIFY (real output, never predicted): go build ./... ; go test ./internal/mcpserver/... -race ; go test ./... -race ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL). Then show a composed body for one of the real bug items that says GitHub issue 26.
SCOPE: internal/mcpserver/gitflow.go and its tests only.
ROLLBACK: the Closes lines are additive text in a pull request body; removing the call restores today's body exactly.

## B-01KYE0RCTXFYVV2C59BVWR4MGN elicitation need responses exit zero over the CLI: an unperformed action reads as success to a shell-driven agent
kind: bug
state: draft
created: 2026-07-26

Found by live judge C: rule op=add with an invalid pattern answers need pattern EARS pattern: U ubiquitous, E event... and exits ZERO over the CLI. The need line is the elicitation fallback — the action was NOT performed — but a shell-driven agent keys on the exit code, reads success, and moves on or retries blind: judge C issued nine rule calls with junk patterns (main, U:fixture, fixture) without ever landing on the plain U the need line names, partly because nothing signaled failure at the machine level. MCP-protocol consumers see the same softness: the result carries no IsError. Analysis: need is designed as a conversational prompt, but on a headless surface an unperformed action that exits 0 violates the same principle the refusal sweep enforced (B-01KYD4H: refusals exit non-zero) — the distinction between refused and needs-more-input does not survive translation to exit codes, and the caller must not be left believing the rule exists. Fix direction: need-fallback results carry IsError and exit non-zero over the CLI, keeping the need grammar as the text so interactive flows still parse the slot question; sweep all elicitSlots call sites. VERIFY: rule op=add with an invalid pattern exits non-zero with the need line as text; the bench script ambiguity-retry machinery is unaffected; a judge rerun shows retry loops keyed by the exit code.
