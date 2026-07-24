---
schema: v0
---

## P-0086 redundant rejections are superseded, never dropped: cluster the learning, retarget the references
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/journal/journal.go

The rejection corpus grows monotonically by design and repeats itself in practice. This repository already carries two rejections that state the same lesson about a task body citing a contract ID that does not exist, and a reader searching before repeating a failed approach pays for both.

The constraint that shapes every option here: reject lines are retained verbatim, no exceptions. Two reasons, both load-bearing. Revocation rebuilds a rejected item from the snapshot stored in its own event, so dropping the event makes the rejection irreversible. And a corpus that could silently lose entries is not worth searching — the whole argument for consulting it before drafting rests on it being complete.

So: supersede, never delete. Redundancy is a property of the NOTE, which carries the learning; the snapshot underneath stays untouched and individually revocable. A redundant rejection is marked as superseded by a canonical one; the event remains in the journal byte for byte, and only the reading surface changes — search returns the canonical entry once, carrying the count and the ids it stands for, instead of returning near-duplicates separately. The invariant survives literally, and the cost it was imposing on readers does not.

Reference retargeting is the second half. Now that items carry a general citation set, an item can cite a rejected item; a citation pointing at a superseded rejection should resolve to the canonical one. Retargeting must be recorded rather than silent, and the original target must stay recoverable — a rewrite nobody can audit is how a graph quietly stops meaning what its authors wrote.

Detection reuses what already exists rather than inventing a second notion of similarity: sentence-token Jaccard at the threshold MCP-005 already applies to mergeable rule pairs. One tuned measure, used twice.

Rejected: folding automatically. MCP-005 set the precedent for exactly this shape of decision and it holds here for a stronger reason — a wrongly merged rule can be split again, while a wrongly superseded rejection hides a lesson precisely when someone is about to repeat it. The dry-run reports clusters; applying them stays a separate, deliberate act.

This proposal's first task delivers the analysis and the supersede representation as pure functions in internal/journal, with no wiring into the compact tool — that file is held by another task in flight, and the split mirrors how internal/knowledge and internal/mcpclient both landed before their tool surfaces.
