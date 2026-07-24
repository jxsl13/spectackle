---
schema: v0
prefix: DRF
---

## DRF-001 {applies: go:mcpserver.Server.stampAnchors,go:drift.Upsert}
WHEN `rule op=edit` or `op=retire` changes a rule's `applies` set, the spectacle server SHALL reconcile anchors.tsv via `drift.Reconcile` — rows of that rule absent from the new set are dropped before the new rows are stamped.

## intent
- P-0024 rule edit must reconcile the anchor set — stale applies rows survive forever: drift.Reconcile shipped: rule add/edit converge anchors to the applies set, retire drops all rows, replay converges identically; live stale IDX-001 row eliminated via re-stamp with the fixed binary
- T-0107 drift: end-line comparison, staleness guard, and a reindex that reindexes: All three defects fixed and verified independently by recomputing hashes, not by trusting the report. DRF-002: RSV-001 now records resolver.go:36-45 with f208f998, matching a recomputation of that exact span. DRF-003: check now refreshes conditionally -- it detects a bound file newer than the index, reindexes once, then classifies; the guard remains as the safety net when reindex fails. Proven live: three comment lines inserted above main() with its body untouched, check run WITHOUT a server restart, classified as a silent move and the anchor refreshed to 65-69 with the correct hash. That same scenario produced a false evolved heal twice before. Defect 3: reindex prints 173 files, 1495 nodes, 1527 edges. Concurrency question resolved: s.g was already serialized by the gate wrapper, no new race.
- P-0077 drift classification is unsound under the resident server: three confirmed defects: delivered by T-0107. TestCheckHealsEvolvedAndReportsRule passes unchanged -- the conditional refresh was the right cure, not a test adjustment.

## DRF-002
WHEN an anchor is classified, the drift classifier SHALL compare the recorded end line as well as the file and start line, so `Result.Class` is never `OK` while `Anchor.End` disagrees with the node's `EndLine`.

Rationale: Classify compares File and Start only. RSV-001 currently records resolver.go:36-44 while go:resolve.Default ends at 45, and check reports ok — the printed range is wrong and nothing detects it.

## DRF-003
IF the symbol graph has not been rebuilt since the classified file last changed on disk, THEN the drift classifier SHALL report the anchor as `pending` instead of computing a `SpanHash` from stale `Line` values.

Rationale: Under the resident -http server the graph is rebuilt only at startup. Hashing a fresh file at stale line numbers produced two false evolved verdicts on go:main.main, each auto-healed, each asserting a review that never happened.
