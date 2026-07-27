---
schema: v1
---

## ADR-01KYGCJ70JESXTEGZJVWF7AXN4 v0.2.0: the full chain landed early - cut the release now or hold to the planned Aug 1-2 window?
kind: adr
state: done
created: 2026-07-26
decision: cut now
status: accepted

kind: radio
option: cut now
option: hold to Aug 1-2
choice: cut now

## B-01KYH5TJ69FW4T2Y2TZZYYK6D8 the reopen edge leaves the pull request ready: every validation round pushes to a non-draft PR
kind: bug
state: draft
created: 2026-07-27
targets: internal/mcpserver/gitflow.go, internal/forge/github.go, internal/forge/offline.go, internal/forge/forge.go

USER OBSERVATION (2026-07-27): unfinished pull requests are not draft-marked. ROOT CAUSE: Open creates drafts and the done edge flips ready (GraphQL, the REST no-op is documented), but the done->active REOPEN edge - every validation-fail round, every compensated archive - never converts the PR back to draft, so the entire repair tail of a task runs against a ready-for-review PR. The policy (CONTRIBUTING: draft at first push, ready AT done, stop pushing) is violated by the machinery on exactly the rounds where pushes resume.

FIX: forge.Forge gains ToDraft(PR) (PR, error) - GitHub via the GraphQL convertPullRequestToDraft mutation (the REST draft field is not writable, same class as Ready); offline forge flips its stored flag. gitFlowFor's to=active arm (the reopen path - it already runs for reopens since the branch exists) finds the branchs open PR and, if not draft, converts it and records g pr N re-drafted (reopened). The done edge already re-readies on the next pass. Never touch PRs of other branches; a missing PR is not an error (records-only items).
NON-NEGOTIABLE, tested: offline e2e - task to done (PR ready), validate-fail reopen (PR draft again, the g line rendered), re-done (ready again), archive (merged); the GitHub ToDraft carries the same doc rationale as Ready re the REST trap; a reopen with no PR stays silent.
VERIFY: build/test -race/vet/gofmt; lint; check ok; the e2e transitions pasted.
SCOPE: the forge interface + two implementations + the reopen arm + tests. ROLLBACK: revert; the interface addition is internal.
REPORT: the e2e state changes verbatim, the GraphQL mutation used.
