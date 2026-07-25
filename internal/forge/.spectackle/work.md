---
schema: v1
---

## B-01KYDE7AX8ES6V6XGYDXA3TJSN Ready reported success while the pull request stayed a draft: REST cannot un-draft
kind: bug
state: done
created: 2026-07-25

DEFECT, found live on the automation's own pull request 40, which it announced as readied and which GitHub still showed as a draft.

CAUSE: GitHub's REST pulls endpoint accepts a PATCH carrying draft:false, answers 200, and leaves the pull request a draft. The field is simply not writable there. Un-drafting is only possible through the GraphQL mutation markPullRequestReadyForReview, which addresses the pull request by node ID rather than by number. Nothing failed and nothing warned, so the caller announced a ready pull request that was not one — the single behavior this package's own comments forbid.

WHY THE TEST DID NOT CATCH IT, and this is the transferable part: the unit test stubbed the PATCH with an httptest handler that returned draft:false, and asserted the stub had been called. A stub that returns what the caller hopes for tests the hope, not the API. The test was green for as long as the feature was broken.

FIX: Ready uses the GraphQL mutation and VERIFIES the result — a response still reporting isDraft true is an error rather than a success. PR carries the node ID for that purpose, and a PR that arrived without one is re-fetched. The rewritten tests pin the mutation and the refusal-to-claim-success separately.

REMAINING RISK, unchanged by this fix: any forge operation whose stub is written from the same assumption as the code has the same blind spot. The offline implementation is a lifecycle double, not a fidelity double, and only a live run tells the two apart.

## B-01KYDJ0205FR7VS9Z4TZY7ZSTV zero check runs just after a push reads as no-CI, so the merge gate can fire before CI starts
kind: bug
state: done
created: 2026-07-25

Observed on the merge gate's first live run, pull request 45. The archive-time records commit was pushed seconds before the gate polled; GitHub Actions had not yet started a run for the new head, the check-runs API answered zero runs, Checks reduced that to ChecksNone, and the gate merged immediately — correct for a repository without CI, wrong for one whose CI simply had not begun. The merge happened to be safe (the same tree had passed the full suite and the branch's earlier heads were green), but the gate did not know that.

CAUSE: zero-runs is ambiguous. It means no CI configured OR CI not started yet, and the two demand opposite behavior — proceed versus wait.

FIX DIRECTION: disambiguate with evidence already available. If any EARLIER head of the same pull request had check runs, the repository demonstrably has CI, and zero runs on the new head means not-started — treat as Pending within a short grace window. Alternatively or additionally, consult the repository's workflow configuration once per process. A grace window alone (treat None as Pending for the first N seconds after a push) is the cheap fallback but wastes the window on genuinely CI-less repositories, which the never-silent records would report as a mysterious wait.

VERIFY: on a repository with CI, a merge gated seconds after a push waits for the run to start and conclude; on a repository without CI, the gate still proceeds without a wait; both paths say what they decided and why.
