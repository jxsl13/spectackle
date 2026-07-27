---
schema: v1
---

## T-01KYJ4Q8DTFPNVDP42SQ7K5SQE bench ledger: record the offline-collapse A/B - commit-only edges save 198 tokens per lifecycle at equal validity
kind: task
state: draft
created: 2026-07-27
targets: docs/bench-curves.md

MEASUREMENT (2026-07-27, EVOLUTION-001): spectackle bench -baseline v0.2.2 -against v0.3.1 over the shared v3 fixture and script - baseline (offline PR theater) 3558B ~889 tokens, candidate (commit-only offline, T-01KYHAH1GJ) 2765B ~691 tokens, delta -793B ~-198 tokens (-22 percent) at equal validity (both sides valid=true), manifest unchanged. Largest per-step savings land on the transition steps that used to render branch/PR/merge lines. TASK: append one ledger entry to docs/bench-curves.md in the established batch-table format citing binaries, fixture version, totals, delta, verdict, and the interpretation (the collapse is measured, not asserted: every offline lifecycle now costs ~198 tokens less). VERIFY: go build ./... && gofmt -l . empty (docs-only change, no code). SCOPE: docs/bench-curves.md append-only. ROLLBACK: revert.
