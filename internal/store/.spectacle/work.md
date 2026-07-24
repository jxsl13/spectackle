---
schema: v0
---

## P-0020 store write batching: cold IndexAll must not pay one commit per file
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/store/store.go, go:index.parseCached

R-0003 measured 21-24s cold for 5000 files - one synchronously committed Exec per Put. Add Flush() error to store.Store (mem no-op); sqlite store buffers Puts in a single transaction committed on Flush/Close; IndexAll calls Flush once after the parse loop. Target: cold 5000-file synth tree well under 3s for the store portion (bench proof).
