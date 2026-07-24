---
schema: v0
---

## intent
- T-0119 journal: cluster redundant rejections and represent supersession, as pure functions: Clustering, canonical selection, supersession representation and citation retargeting land as pure functions; journal.go byte-unchanged. Jaccard was reimplemented rather than reused because mcpserver imports journal, so reusing would be a cycle -- called out in the code as instructed rather than silently retuned. The invariant guard passes because ClusterRedundant returns singleton clusters instead of dropping non-matching events, which is exactly what makes the never-drop property testable. Wiring the dry-run and apply paths into compact stays open: tools.go was held all round.
- P-0086 redundant rejections are superseded, never dropped: cluster the learning, retarget the references: analysis delivered by T-0119, dry-run reporting by T-0123. The apply path stays deliberately unbuilt: superseding is a second act, and no journal write exists for it.
