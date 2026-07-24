---
schema: v0
prefix: DRF
---

## DRF-001 {applies: go:mcpserver.Server.stampAnchors,go:drift.Upsert}
WHEN `rule op=edit` or `op=retire` changes a rule's `applies` set, the spectacle server SHALL reconcile anchors.tsv via `drift.Reconcile` — rows of that rule absent from the new set are dropped before the new rows are stamped.