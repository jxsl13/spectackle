---
schema: v0
---

## P-0078 general item-to-item references, so deliberation chains exist and grill can demand them
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/item/item.go

Two gaps, one cause. Items can only be linked upward and singly: Parent is one string meaning task-under-proposal, and Needs is reserved for blocking ADRs minted by escalation. There is no general reference set, so a research item cannot cite another research item, a proposal cannot point at the research that produced it, and an ADR cannot name the research that fed the decision. Such citations survive today only as an ID typed into body prose — findable by full-text search, but not an edge, so get depth cannot traverse it and nothing can verify the target exists.

Second gap, downstream of the first: nothing produces or requires a weighed plan before implementation. research aggregates read-only, draft records, grill critiques (#targets #contracts #briefs #tests #rejections #questions). The deliberation — which approaches existed, why one won — lives as unstructured prose in the proposal body if it is written down at all.

Rejected: fold planning into grill, making it a plan-and-grill step. Grill's value is that it works against the author; a step that both writes the plan and critiques it loses exactly that independence. Also rejected: a new record kind for plans. The kind already exists — an ADR is literally question, options, decision, consequences, and an implementation-approach choice is that same shape at a lower altitude. Minting a near-duplicate kind would split one concept across two vocabularies.

So: add the missing edge, then let grill ask for it. A Refs field (list of item IDs, any kind to any kind, validated to exist) makes deliberation chains representable; grill gains a question when a proposal records no weighing — no linked ADR or research, and no rejected-alternative content — which is the cheap version of demanding a plan without building a plan step.

This task delivers the storage layer only: the field, its round-trip through work.md, validation, and tests. The grill question and the draft/get surfacing follow once internal/mcpserver/tools.go is free — a sibling task holds it right now.

## T-0109 item.Refs: a general reference set with round-trip and existence validation
kind: task
state: active
created: 2026-07-24
parent: P-0078
targets: internal/item/item.go, internal/item/item_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
Give items a general reference set so any item can cite any other — research citing research, a proposal citing the research that produced it, an ADR naming the research that fed it. Today only Parent (one string, task-under-proposal) and Needs (reserved for blocking ADRs from escalation) exist, so citations survive only as an ID typed into body prose: findable by full-text search, but not an edge.

SCOPE (disjoint, lease exactly these two)
  internal/item/item.go
  internal/item/item_test.go
Do NOT touch internal/mcpserver (a sibling task holds tools.go, server.go and state.go), internal/drift, internal/lifecycle, cmd/spectackle, internal/knowledge, README.md or docs/. The draft/get tool surface and the grill question come in a follow-up task once tools.go is free — this task is the storage layer only, and it must compile and pass with NO caller changes anywhere.
.spectackle files are server-owned: never edit them by hand.

WHAT TO ADD
A Refs []string field on item.Item, holding item IDs, any kind to any kind, order-preserving and duplicate-free.
Place it with Parent and Needs, not among the ADR template fields, and document in its doc comment how it differs from both: Parent is a single structural owner (one task belongs to one proposal); Needs means blocked-on and drives the escalation exits; Refs is a plain citation with no lifecycle meaning at all. That distinction is the reason for a third field rather than reusing Needs — overloading Needs would make unrelated citations look like blockers and change how escalation behaves.

SERIALIZATION
work.md is the store; items are ## -anchored blocks with header lines like parent: <id>. Follow the existing pattern exactly (see how Parent is written around item.go:269 and parsed around item.go:148, and how Needs handles a list). Emit the field only when non-empty, exactly as Parent does — an empty Refs must leave the rendered block byte-identical to today, which is what keeps this change invisible to every existing record.
Round-trip is the load-bearing property: Parse(Render(it)) must deep-equal it, including Refs order. Prove it with a test over an item carrying several refs, not just one.

VALIDATION
Provide an exported helper that checks a proposed reference set against a set of known item IDs and reports the unknown ones. Signature roughly:
  func UnknownRefs(refs []string, known map[string]bool) []string
Do NOT make Parse reject unknown refs. A work.md may legitimately cite an item that is archived out of work.md, and a parser that refuses to load such a file would make a dangling citation unrecoverable. Validation belongs at the write path, which is the follow-up task's job; this task supplies the helper and the tests for it.
Also validate shape: a ref must match item.IDRe. Self-reference (an item citing its own ID) must be reported as invalid — it is always a mistake and cheap to catch here.

TESTS (internal/item/item_test.go, matching the file's existing style)
  1. round-trip with several refs, order preserved.
  2. empty Refs renders byte-identically to an item without the field — the backward-compatibility guard.
  3. parsing a work.md written before this change (no refs line) yields an empty Refs and no error.
  4. duplicate refs are collapsed, order of first appearance kept.
  5. UnknownRefs: reports exactly the IDs missing from the known set, in input order; empty result for a fully-known set.
  6. malformed ref (fails IDRe) and self-reference are both reported.

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/item/... -race -v
  go test ./...
  go vet ./internal/item/...
  /home/user/spectackle/bin/spectackle lint
go test ./... must be green WITHOUT touching any other package — if adding the field breaks a caller, that is a signal you changed more than a field, so report it instead of editing the caller.

EXIT CRITERION
All six tests green under -race, ./... green with no changes outside internal/item, vet clean, lint clean, and an item with no refs rendering byte-identically to today.

ROLLBACK
One optional field, written only when non-empty, plus one pure helper. Existing work.md files parse unchanged and re-render unchanged. Deleting the field and the helper restores the prior state; no schema stamp change, no migration, no record rewrite.

REPORT BACK
The field's final shape and doc comment, each test's real output, the byte-identity evidence for the empty case, and anything you deliberately did NOT do.
