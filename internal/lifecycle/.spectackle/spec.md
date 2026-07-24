---
schema: v0
prefix: LCY
---

## LCY-001 {applies: go:lifecycle.archive,go:item.Get,go:mcpserver.Server.getItem}
WHEN an item reference names an ID absent from every work.md, the spectacle server SHALL resolve it via `lifecycle.Tombstone` from journal `archive` events as a read-only archived item, accepted by `decide item=`, `get` and `draft parent=`.

Rationale: archived is a rest position, not oblivion — provenance links to finished research/proposals must survive archiving (found live: decide item=R-0004 failed after archive)
## LCY-002 {applies: go:mcpserver.Server.openNeeds,go:mcpserver.hasOpenNeeds}
WHEN a `needs:` entry names an archived item, the spectacle server SHALL count that need as satisfied in `openNeeds` and `hasOpenNeeds` — an archived dependency is finished work, never an open blocker.

## intent
- P-0021 archived items stay referenceable: journal tombstones + decide option fidelity: archived items resolve as journal tombstones at every reference site; decide options roundtrip byte-exact
