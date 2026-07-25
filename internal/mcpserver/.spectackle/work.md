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

## T-01KYDBWE98FPZ9X5X714RV2ZJQ couple the state transitions to the git workflow in the server
kind: task
state: done
created: 2026-07-25
parent: P-01KYDBRWFZFXSBRF4PNRD6R4D9
refs: ADR-01KYDBQGMRFBN9SAHCWWNSAKX4
targets: internal/mcpserver/swarm.go, internal/mcpserver/tools.go

IMPLEMENTER IN OWN WORKTREE. BLOCKED-ON: both siblings under P-01KYDB (internal/forge, and the config plus git primitives) must be MERGED first — this task is the wiring and has nothing to call until they exist. Verify with find scope=code before starting; if either is absent, stop and report.

WHAT TO BUILD: the transition mapping, and only it. The LLM must never have to issue a git command again — that is the requirement's whole point, since every such command is tokens spent on mechanics instead of judgment.
  work op=start, or a move into active: ensure the task branch, commit code, push, open a DRAFT pull request whose title carries the full task ID.
  any server write while a task is active: commit and push, so no change is ever only local. Debounce it the way the compact and stale hints already debounce their work — a push per journal append is unacceptable — and say in your report what you chose and why.
  move to done: flip the pull request to ready for review.
  archive after verification: merge, merge commit, never squash.

IDEMPOTENCE IS THE CORRECTNESS PROPERTY. Every step must be safe to repeat: an existing branch is reused, an existing pull request is found rather than duplicated, an already-ready pull request is left alone, an already-merged one is not merged twice. Tool calls retry and agents crash mid-sequence; a mapping that only works on a clean first run will produce duplicate pull requests in normal use.

DEGRADATION: everything the forge cannot do is reported as a record on the tool result, never as a failed state transition — except that the automation must not claim success it did not have. Insufficient permission on merge leaves the pull request open and says so (ADR-01KYDB). Offline mode runs the same sequence against the local repository. Feature disabled in config restores today's behavior exactly, which is the assertion that protects every existing user on upgrade.

THE CONSTRAINT THAT OVERRIDES CONVENIENCE, from B-0006, SPX-SWM-001 and a prior rejection of this exact idea: record state reaches the default branch through the semantic replay, never through a git merge of journal files. Commit and push CODE. Never git-merge .spectackle between branches. The sibling primitives already enforce this; do not route around them.

TESTS: the full active-to-archived sequence against the offline implementation, asserting branch, draft, ready and a local merge commit (assert the merge is a merge commit — two parents — not a fast-forward, since never-squash is the point); every step repeated twice and shown to be a no-op the second time; the feature disabled producing byte-identical output and no git side effects; a merge refused for permission leaving the pull request open with a record naming why; and the existing work op=start and submit tests passing UNCHANGED, since this must extend that flow rather than replace it.

VERIFY (real output, never predicted): go build ./... ; go test ./internal/mcpserver/... -race ; go test ./... -race ; go vet ./... ; gofmt -l . (empty) ; spectackle lint . (POSITIONAL) ; spectackle call -root . check '{}' ends exactly ok. Then drive it live in OFFLINE mode over a scratch repository and paste the resulting git log.
SCOPE: the two named files plus their tests, and docs/tools.md if any tool schema or record shape changes (SPX-REPO-001 binds them). Do NOT touch internal/forge or internal/wt beyond calling them, and no file under any .spectackle directory.
ROLLBACK: the coupling is behind the config flag; disabling it is the rollback, and removing the call sites is the full one.
REPORT BACK: the debounce you chose for the during-work push and why, how idempotence is enforced per step, the live offline transcript, each verification command's real result, anything deliberately not done.
