---
schema: v1
---

## B-01KYDE7AX8ES6V6XGYDXA3TJSN Ready reported success while the pull request stayed a draft: REST cannot un-draft
kind: bug
state: active
created: 2026-07-25

DEFECT, found live on the automation's own pull request 40, which it announced as readied and which GitHub still showed as a draft.

CAUSE: GitHub's REST pulls endpoint accepts a PATCH carrying draft:false, answers 200, and leaves the pull request a draft. The field is simply not writable there. Un-drafting is only possible through the GraphQL mutation markPullRequestReadyForReview, which addresses the pull request by node ID rather than by number. Nothing failed and nothing warned, so the caller announced a ready pull request that was not one — the single behavior this package's own comments forbid.

WHY THE TEST DID NOT CATCH IT, and this is the transferable part: the unit test stubbed the PATCH with an httptest handler that returned draft:false, and asserted the stub had been called. A stub that returns what the caller hopes for tests the hope, not the API. The test was green for as long as the feature was broken.

FIX: Ready uses the GraphQL mutation and VERIFIES the result — a response still reporting isDraft true is an error rather than a success. PR carries the node ID for that purpose, and a PR that arrived without one is re-fetched. The rewritten tests pin the mutation and the refusal-to-claim-success separately.

REMAINING RISK, unchanged by this fix: any forge operation whose stub is written from the same assumption as the code has the same blind spot. The offline implementation is a lifecycle double, not a fidelity double, and only a live run tells the two apart.
