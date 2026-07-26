---
schema: v1
---

## B-01KYG56YQAFD09C2151VVYQHSV archive split the record from the code: closure heuristics discard live work
kind: bug
state: draft
created: 2026-07-26
grilled: 2026-07-26 open=0
targets: internal/mcpserver/gitflow.go

ROOT CAUSE CONFIRMED (gitflow.go gitFlowMerge, the B-01KYDY guard): when the item branch exists but is not the current branch, the flow unconditionally closes on a fresh -close branch at the current head and never touches the item branch. That guard was designed for STALE-ERA branches (checking one out would rewind the tree), but it cannot tell a stale branch from a LIVE one carrying validated unmerged code - and after a refusal sequence left the residents checkout elsewhere, it fired on T-01KYFXEQs live branch: PR 143 merged records only, PR 142 with the implementation stayed open, main lacked the code until a manual escape-hatch merge. The user flagged the two open PRs; the meta-lesson they drew is part of this record: per-diff computed classes cannot see cross-feature interaction bugs - the flow must check its own post-conditions mechanically instead of relying on reviewer completeness.

SECOND INCIDENT, same family, while filing this very bug: the first draft of this record (B-01KYG4PC, created on the residents main checkout between two closures) VANISHED when the next archive closure (PR 144) reset the local main to origin - draft records edge-committed locally between merges are unpushed and a closure checkout discards them. The record you are reading is the re-draft; the loss is itself evidence for the post-condition theme below.

FIX, in gitFlowMerge:
1. DISCRIMINATOR: in the exists-but-not-current arm, test whether base already contains the branch tip (git merge-base --is-ancestor <branch> <base>, LC_ALL=C, exit-code structural - never prose-parsed). Ancestor -> genuinely stale era -> the -close records-only path stands unchanged. NOT ancestor -> the branch carries unmerged work: CHECK IT OUT and run the normal closure on it (records commit, push, PR flip/merge) - exactly what the lifecycle does when the branch is current; the rewind concern only ever applied to already-merged eras.
2. POST-CONDITION SELF-CHECK (the meta-lesson made mechanical): after the merge step of any archive closure, query the forge for open PRs referencing the item branch (both naming eras via the short-prefix match); any hit renders ! GIT E <id> stranded pr <n> - validated code left unmerged. Loud, zero LLM tokens, fires regardless of which future heuristic misroutes.
3. LOCAL-STATE PRESERVATION: before any closure checkout that moves the working tree off the current branch, the flow must carry unpushed local commits on the default branch forward (rebase the fresh checkout onto them or refuse naming them) - never silently reset local main to origin (the vanished-draft incident).

NON-NEGOTIABLE, tested (e2e fixture repos, offline forge, the existing gitflow_test patterns): (a) live-branch shape - item branch has a code commit absent from main, current branch is main: archive merges THE ITEM PR, main contains the code, zero open PRs reference the item; (b) stale-era shape - item branch fully contained in main: -close records-only path unchanged (existing TestArchiveWithStaleItemBranchUsesClosureBranch stays green); (c) the post-condition check fires on a synthetic stranded state and stays silent on a clean closure; (d) never-active shape (no branch) unchanged.
VERIFY: go build/test -race/vet/gofmt; lint; check ok; the three archive-flow e2e tests pasted.
SCOPE: gitFlowMerge + one ancestor helper (wt package if a sibling exists there) + tests. No lifecycle.go, no forge interface changes beyond reusing Find.
ROLLBACK: revert; the discriminator only widens the path that already existed for the current-branch case.
REPORT: the discriminator command verbatim, each tests result, the stranded-check line format.
