---
schema: v1
---

## T-01KYD88M80EQEAJDW0AB243ZK2 research return path enforced at the archive gate: an R-item archives only consumed or explicitly closed
kind: task
state: done
created: 2026-07-25
parent: P-01KYD87FX0F6YRX49R3A8TB6E4
refs: R-0007, T-01KYD72HNHEYAB0WF42BTR31CW
rounds: 2
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
