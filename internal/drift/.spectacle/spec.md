---
schema: v0
prefix: DRF
---

## DRF-001 {applies: go:mcpserver.Server.stampAnchors,go:drift.Upsert}
WHEN `rule op=edit` or `op=retire` changes a rule's `applies` set, the spectacle server SHALL reconcile anchors.tsv via `drift.Reconcile` — rows of that rule absent from the new set are dropped before the new rows are stamped.

## intent
- P-0024 rule edit must reconcile the anchor set — stale applies rows survive forever: drift.Reconcile shipped: rule add/edit converge anchors to the applies set, retire drops all rows, replay converges identically; live stale IDX-001 row eliminated via re-stamp with the fixed binary
