---
schema: v1
---

## intent
- B-01KYDJ0205FR7VS9Z4TZY7ZSTV zero check runs just after a push reads as no-CI, so the merge gate can fire before CI starts: zero check runs on a fresh head now disambiguate through the workflows API: a repository with active workflows reads empty as not-started (Pending, so the gate waits), one without reads it as no-CI (None, so the gate proceeds); the lookup is cached per client
- T-01KYDKNR0SF3XA0FECWZYFMKVX forge transport retries network and 5xx failures with backoff inside a five minute budget before surfacing: transport failures and 5xx retry with doubling backoff inside a five minute budget at the one seam every forge operation shares; 4xx is never retried since those are semantic answers the callers classify; a failure surviving the budget names its attempt count. Found in testing: a deliberate-5xx unit test silently ran the production budget and stretched the package to four minutes, so the shared test constructor now shrinks the budgets for every test
