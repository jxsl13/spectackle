---
schema: v0
prefix: DRF
---

## DRF-001 {applies: go:mcpserver.Server.stampAnchors,go:drift.Upsert}
WHEN `rule op=edit` or `op=retire` changes a rule's `applies` set, the spectacle server SHALL reconcile anchors.tsv via `drift.Reconcile` — rows of that rule absent from the new set are dropped before the new rows are stamped.

## intent
- P-0024 rule edit must reconcile the anchor set — stale applies rows survive forever: drift.Reconcile shipped: rule add/edit converge anchors to the applies set, retire drops all rows, replay converges identically; live stale IDX-001 row eliminated via re-stamp with the fixed binary

## DRF-002
WHEN an anchor is classified, the drift classifier SHALL compare the recorded end line as well as the file and start line, so `Result.Class` is never `OK` while `Anchor.End` disagrees with the node's `EndLine`.

Rationale: Classify compares File and Start only. RSV-001 currently records resolver.go:36-44 while go:resolve.Default ends at 45, and check reports ok — the printed range is wrong and nothing detects it.

## DRF-003
IF the symbol graph has not been rebuilt since the classified file last changed on disk, THEN the drift classifier SHALL report the anchor as `pending` instead of computing a `SpanHash` from stale `Line` values.

Rationale: Under the resident -http server the graph is rebuilt only at startup. Hashing a fresh file at stale line numbers produced two false evolved verdicts on go:main.main, each auto-healed, each asserting a review that never happened.
