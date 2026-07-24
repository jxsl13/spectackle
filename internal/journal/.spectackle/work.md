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

## T-0119 journal: cluster redundant rejections and represent supersession, as pure functions
kind: task
state: active
created: 2026-07-24
parent: P-0086
targets: internal/journal/redundant.go, internal/journal/redundant_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/journal/journal.go before writing anything — you are adding to a package with a hard invariant, stated in its own doc comment.

THE INVARIANT YOU MUST NOT BREAK
The journal is append-only with one sanctioned exception: compaction may fold ordinary events, but reject, archive and compact lines are ALWAYS retained verbatim. Two reasons, both load-bearing: revocation rebuilds a rejected item from the snapshot carried in its own event (fields Body, Tg, Par, Rnd, Gr), so dropping the event makes the rejection irreversible; and a corpus that could silently lose entries is not worth searching, which is the entire argument for consulting it before drafting.
This task therefore adds NO deletion path. Nothing you write may remove, rewrite or reorder an existing event. If you find yourself designing one, you have misread the task.

GOAL
Pure analysis plus a representation for supersession. Redundancy is a property of the NOTE, which carries the learning; the snapshot underneath is untouched and stays individually revocable. Superseding changes what a READER sees, never what is stored.

SCOPE (the files are NEW; lease exactly these two)
  internal/journal/redundant.go
  internal/journal/redundant_test.go
Do NOT modify internal/journal/journal.go — put everything in the new file. Do NOT touch internal/mcpserver (two sibling tasks hold tools.go, grill.go, decide.go and internal/knowledge right now), internal/item, internal/drift, cmd/, README.md or docs/. There is NO tool wiring in this task: the compact tool lives in tools.go, which is held. .spectackle files are server-owned: never edit them by hand.

WHAT TO BUILD
1. Clustering. Given a slice of reject events, group those whose NOTES express the same learning.
   Similarity: sentence-token Jaccard at threshold 0.6 — the same measure and threshold MCP-005 already applies to mergeable rule pairs (read that rule and the code implementing it, then use one notion of similarity rather than inventing a second). If the existing implementation is not reachable from this package without an import cycle, reimplement it identically and say so in your report; do not silently tune the threshold.
   Compare notes only. Do NOT compare item bodies, titles or ids — two different items rejected for the same reason is exactly the case worth clustering.
   Deterministic: same input, same clusters, same order. Sort before returning.
2. Canonical selection. Each cluster names one canonical event and the ones it stands for. Pick deterministically and document the rule in a comment — earliest wins is defensible (the first statement of a lesson is the one later ones repeat) but say why you chose whatever you choose.
3. Supersession representation. A value type expressing: canonical id, superseded ids, and the retargeting each superseded id implies for anything citing it. Marshalable to a journal event of the existing compact kind or an adjacent one — but DO NOT append anything in this task, only represent it.
4. Reference retargeting, as a pure function. Given a set of citations (item id to the ids it cites) and a supersession set, return the rewritten citations plus a record of every rewrite performed. The record is not optional: a rewrite nobody can audit is how a graph quietly stops meaning what its authors wrote. The original target must be recoverable from the record.

WHAT NOT TO DO
No automatic folding, no I/O, no journal writes, no cache updates. This package half is analysis only; a later task wires the dry-run report and the apply path once tools.go is free.

TESTS (redundant_test.go, table-driven)
  1. two rejections whose notes state the same lesson in different words cluster together above the threshold; two unrelated ones do not.
  2. determinism: the same input twice yields byte-identical cluster output including order.
  3. clustering ignores item identity: two different item ids, same note, still cluster.
  4. canonical selection follows your documented rule, proven on a cluster of three.
  5. retargeting rewrites citations pointing at a superseded id to the canonical one, leaves others alone, and reports every rewrite with its original target.
  6. the invariant guard: assert that nothing in your API can produce a shortened event slice — pass a slice in, and assert every input event id is still present in whatever you return or reference. This is the test that fails if someone later adds a deletion path.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/journal/... -race -v
  go test ./...
  go vet ./internal/journal/...
  /home/user/spectackle/bin/spectackle lint

EXIT CRITERION
Six tests green under -race, the invariant guard passing, ./... green, vet clean, lint clean, and internal/journal/journal.go byte-unchanged.

ROLLBACK
One new file, imported by nothing until the wiring task. Deleting it restores the prior state exactly; no schema, stored format, record or anchor change.

REPORT BACK
The API, your canonical-selection rule and why, whether you reused or reimplemented the Jaccard measure and why, each test's real output, and anything you deliberately did NOT do.
