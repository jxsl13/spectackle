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
- B-01KYS1028RE8NTN07F6HSGBRPH capGist doc comment is attached to the wrong symbol, its ordering contract is unpinned, and its comment overstates the coverage: validated pass by verifier-capgist diff 94c3631cc473 :: C1: go doc -u capGist and gistLineEndings each print their OWN non-empty paragraph (verified). Scratch-clone go-doc diff between parent 8c786b1 and delta 3d86427 shows ONLY gistLineEndings and capGist docs changed; no other symbol affected. C2: reordering the replacer to LF-first (or ReplaceAll variants matching it) makes TestCapGistCollapses [body truncated at tombstone retention cap]
- B-01KYRQY892FSDSN75P9FFXFDM5 summary truncates on a raw byte slice and can emit invalid UTF-8, and status rides the prose composer untrusted on restore: validated pass by verifier-trunc diff 721d102b69c1

## LC-001 {applies: go:lifecycle.retainedBody,go:lifecycle.archive}
The archive step SHALL retain the full body in the journal tombstone for every record kind whose body is its outcome rather than a delta merged into spec.md.

Rationale: A proposal, task or bug compacts fairly because its change landed in the code and the spec; research and adr have no such delta, so compressing them to a summary deletes the only copy. Found twice: research lost 268 findings their citations, then decide-minted ADRs archived to the byte-identical contentless string adr <question> - kind: radio, erasing both sides of every curated conflict. An ADR also keeps its outcome in item fields the journal event has no channel for, so the body alone is not enough.
