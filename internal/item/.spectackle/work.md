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
