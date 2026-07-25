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

## T-01KYDKNR0SF3XA0FECWZYFMKVX forge transport retries network and 5xx failures with backoff inside a five minute budget before surfacing
kind: task
state: done
created: 2026-07-25
refs: ADR-01KYDGXWH4FX9VQTG0G2CF8GQQ

User requirement, stated twice in quick succession so it is firm: the MCP retries with backoff for up to five minutes when network problems occur, and only then passes the error through.

WHAT TO BUILD: retry at the transport seam — GitHub.request and GitHub.graphql — so every forge operation inherits it without per-call-site duplication. Retry on transport errors (the http.Client.Do error path: refused connections, resets, DNS) and on 5xx responses; NEVER on 4xx, which are semantic answers the callers already classify (403 permission, 405 and 409 transient merge states, 422 validation). Backoff doubles from one second capped at sixty, budget five minutes as a package variable the tests shrink to milliseconds.

NEVER-SILENT accounting: a failure that survives the budget surfaces as one error naming the attempt count and the budget, so the operator sees it was retried rather than dropped on first contact. A success after retries is a succeeded action, not a suppressed one, so it carries no extra record — noted here deliberately as the agreed boundary of ADR-01KYDG rather than an oversight.

TESTS: an httptest server failing twice with 503 then succeeding — the operation succeeds and the workflow proceeds; an always-503 server — the surfaced error names attempts and budget; a 422 is NOT retried, asserted by call count; a closed listener (real transport error) retries and then surfaces; budgets shrunk via the package variables.

VERIFY: go build ./... ; go test ./internal/forge/... -race ; go test ./... -race ; go vet ; gofmt -l (empty).
SCOPE: internal/forge/github.go and tests. The offline implementation has no transport and is untouched.
ROLLBACK: the retry wrapper is one function around the two HTTP call sites.
