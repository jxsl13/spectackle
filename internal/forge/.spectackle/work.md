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

## B-01KYDN1BMXFAMB773EE7SH2B1P the CI verdict is polled by branch name, so seconds after a push it can belong to the previous head
kind: bug
state: draft
created: 2026-07-25

Observed on the archive of the severity-taxonomy fix: the archive-time records commit was pushed and the merge gate reported checks passing within nine seconds — faster than this repository's CI has ever concluded. The verdict it read almost certainly belonged to the PREVIOUS head: Checks queries the check-runs API by branch NAME, and immediately after a push the forge can still resolve that ref to the old commit, whose runs are concluded and green. Same defect class as the zero-runs ambiguity fixed earlier (B-01KYDJ), from the opposite direction — that one merged before CI started, this one can merge on the predecessor's green.

RISK, honestly bounded: the final push before an archive is normally the records commit, which touches no code, so the predecessor's verdict is usually the right verdict for the tree being merged. Usually is not always: a done-transition code checkpoint followed quickly by archive would merge on the checkpoint predecessor's green.

FIX DIRECTION: poll by the exact head SHA rather than the branch name. The pusher knows the SHA it pushed (rev-parse after push); PR gains a HeadSHA the gitflow fills, and Checks queries commits/<sha>/check-runs, which cannot be resolved to any other commit. Zero-runs-for-that-sha then genuinely means not-started, tightening the B-01KYDJ disambiguation as well.

VERIFY: after a push, the gate polls the pushed SHA and waits for ITS runs to conclude; the branch-lag window provably cannot return the predecessor's verdict (unit: scripted forge keyed by SHA; live: an archive immediately after a push waits the full CI duration instead of nine seconds).
