---
schema: v0
prefix: SPX-GRA
---

# Symbol graph contracts

## SPX-GRA-001
The graph SHALL identify every node by an ID of the form `lang:name` that is byte-identical across re-indexing runs of unchanged files.

Rationale: stable IDs are the currency the LLM uses to reference code cheaply.

## SPX-GRA-002
WHEN Impact is invoked, the graph SHALL return each reachable node exactly once at its minimum BFS distance from the seed set.

## SPX-GRA-003 {applies: go:graph.memGraph.Find}
The graph SHALL match Find queries against node ID, file path and signature, ranking ID matches above file- or signature-only hits.

Rationale: an agent knows a file name or a parameter type as often as a symbol name; ID hits stay the cheapest currency.