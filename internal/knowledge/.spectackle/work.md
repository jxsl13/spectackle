---
schema: v0
---

## P-0076 cross-repo knowledge: extract portable records, condense by recurrence, apply additively
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/spec/cascade.go, internal/item/item.go

Goal: lift the reusable part of one repository's records into a portable artifact, merge several such artifacts into a condensate, and apply that condensate additively to every participating repository so a fleet converges on shared concepts, conventions, process models and library choices.

Central design decision, and the one that makes this buildable: extraction does NOT attempt to generalize prose. Rewriting an EARS sentence from repo-specific to generic is a natural-language judgment no mechanical pass can make reliably, and a tool that silently paraphrases contracts would corrupt the corpus it is meant to spread. Instead genericity is MEASURED, not asserted: extraction is a mechanical de-anchoring that keeps every sentence verbatim and attaches provenance, and merge treats RECURRENCE ACROSS REPOSITORIES as the genericity signal. A convention five of six repositories independently assert is a standard; one that appears once is local color. The condensate ranks by that count, so a human curates a ranked list instead of auditing paraphrases.

What travels: rules (EARS sentences), ADRs (question, options, decision, consequences, status) and intent prose. What does not: proposals, tasks and bugs. Those are work items describing one repository's transient state, not knowledge, and shipping them would drown the condensate in noise.

What is stripped at extraction, because it cannot mean anything in another repository: applies anchors (node IDs), anchor rows, rule ID prefixes and numbers, and lifecycle state. Consequence to state plainly rather than hide: an applied rule lands unanchored, so check reports it as a coverage gap until someone binds it to real nodes in the receiving repo. That is the correct behavior — the gap list is exactly the adoption worklist — but it means apply increases reported gaps on purpose.

Merge must never silently pick a winner. Two repositories asserting contradictory rules is a real disagreement about how they should work; auto-resolution would erase it. Contradictions surface as records for a human, and the natural home for the resolution is an ADR in the receiving repo.

Apply is additive and idempotent: it adds what is missing, never deletes, never overwrites a local specialization, and routes every write through the existing rule and decide paths so no new file-write path enters the codebase.

Format: markdown with front matter, the same family as spec.md, schema-stamped. A condensate exists to be read and curated by a human before it is pushed to N repositories, which rules out an opaque binary or a database; and the repo already owns a parser for exactly this shape.

Split: this proposal's first task delivers the portable format plus extraction plus merge as a standalone package with no tool wiring, mirroring how internal/mcpclient landed before its subcommand. Apply and the tool surface follow once that package is stable, so the tool count stays minimal and orthogonal — the intent is ONE tool with extract, merge and apply operations, not three tools.
