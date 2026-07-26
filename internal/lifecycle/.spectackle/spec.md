---
schema: v1
prefix: LCY
---

## LCY-001 {applies: go:lifecycle.archive,go:item.Get,go:mcpserver.Server.getItem}
WHEN an item reference names an ID absent from every work.md, the spectacle server SHALL resolve it via `lifecycle.Tombstone` from journal `archive` events as a read-only archived item, accepted by `decide item=`, `get` and `draft parent=`.

Rationale: archived is a rest position, not oblivion — provenance links to finished research/proposals must survive archiving (found live: decide item=R-01KY9MRDB8F6KBJGFH2AQH45SD failed after archive)
## LCY-002 {applies: go:mcpserver.Server.openNeeds,go:mcpserver.hasOpenNeeds}
WHEN a `needs:` entry names an archived item, the spectacle server SHALL count that need as satisfied in `openNeeds` and `hasOpenNeeds` — an archived dependency is finished work, never an open blocker.

## intent
- P-01KY9MZ098EM4AXJ6BEGCBRBD6 archived items stay referenceable: journal tombstones + decide option fidelity: archived items resolve as journal tombstones at every reference site; decide options roundtrip byte-exact
- T-01KYDAS36XFHJ8X3H1Q8Y845FF exhaustive, deterministic transition matrix over every state pair: internal/lifecycle/matrix_test.go exercises every ordered state pair deterministically: forward skips legal, backward hops refused except the reopen, side states entered only by their designated mechanisms; green in every suite run since landing
