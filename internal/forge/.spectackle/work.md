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

## B-01KYDV9PWZFEQV4VR3NRJ42R0F offline forge Ready mutates memory only: the draft flip never reaches forge-offline.json
kind: bug
state: active
created: 2026-07-25

Found by the enriched bench fixture (T-01KYDT) during the corrected re-measurement of the B-01KYDS merge: post-merge runs pay a systematic +49 bytes on the archive step because the pull request arrives at archive still draft and the merge path flips it a second time (g local gates passed plus g pr ready lines appear twice per lifecycle). Isolated cause, confirmed by reading internal/forge/offline.go: Open persists via o.save() and Merge persists after delete, but Ready sets rec.Draft = false and returns without saving — the flip lives only in that process. Every spectackle call is its own process, so the next Find loads Draft=true from forge-offline.json. Masked until B-01KYDS because nothing consulted pr.Draft after done. Observed vs expected, reproduced with a minimal offline lifecycle: archive output carries the duplicate gate-and-ready pair; expected: done flips once, archive sees Draft=false and goes straight to merge. Fix: o.save() after the flip inside Ready, under the held mutex, symmetric with Open and Merge. VERIFY: unit test drives Open, Ready, then reopens the state file in a NEW Offline instance and asserts Find reports Draft=false; the minimal offline lifecycle probe shows the archive step without the duplicate pair; bench archive step returns to its pre-B-01KYDS byte cost. Side observation for B-01KYDK: the offline merge hardcodes user.name=spectackle user.email=spectackle@localhost, same identity class as internal/wt.
