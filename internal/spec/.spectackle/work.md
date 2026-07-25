---
schema: v0
---

## B-01KYD1G9SJFE8B1CGF6THPX6J4 rule op=edit drops the blank-line separator before the next rule, the loss accumulates, and lint does not notice
kind: bug
state: draft
created: 2026-07-25
targets: internal/spec/author.go

GitHub issue 30, filed alongside issue 25 against the same edit path.

OBSERVED: editing a rule removes the blank line separating it from the following rule. The loss is permanent and accumulates, so a bundle degrades progressively toward one unseparated block while lint reports zero findings throughout. add and edit therefore produce different byte layouts for identical logical content: whether a rule carries a separator depends on its edit history rather than on its content.

WHY IT IS MORE THAN COSMETIC, per the reporter: each edit touches a line belonging to the FOLLOWING, unrelated rule, which widens the surface for merge conflicts between agents on a shared bundle — and the journal already carries a union merge attribute, so conflict avoidance is demonstrably a design concern here. CommonMark accepts a heading without a preceding blank line so it renders today, but stricter markdown linters flag it, which is how it was noticed. And it is silent: neither lint nor check reports it.

FIX DIRECTION: the serializer emits one canonical layout regardless of how a rule reached its current text, so add and edit converge on identical bytes for identical content. Consider whether lint should assert canonical layout, since a formatting invariant nothing checks will drift again.

VERIFY: the reproduction — three added rules, then edits to the first and second — leaves a file byte-identical to one where the same three rules were added with their final text directly.
