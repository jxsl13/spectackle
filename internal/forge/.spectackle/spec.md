---
schema: v1
---

## intent
- B-01KYDJ0205FR7VS9Z4TZY7ZSTV zero check runs just after a push reads as no-CI, so the merge gate can fire before CI starts: zero check runs on a fresh head now disambiguate through the workflows API: a repository with active workflows reads empty as not-started (Pending, so the gate waits), one without reads it as no-CI (None, so the gate proceeds); the lookup is cached per client
