---
schema: v0
---

## intent
- T-0109 item.Refs: a general reference set with round-trip and existence validation: item.Refs live: general citation set, any kind to any kind, no lifecycle meaning. Emitted only when non-empty so every existing record renders byte-identically; Parse never rejects an unknown ref because a citation may point at an item archived out of work.md. Implementer added a selfID parameter to UnknownRefs -- correct, the two-arg form cannot detect self-citation. Wiring into draft/get and the grill question follows once tools.go is free.
- P-0078 general item-to-item references, so deliberation chains exist and grill can demand them: storage layer delivered by T-0109 (item.Refs). Wiring Refs into draft/get and the grill question that demands a recorded weighing remain open -- they need tools.go and grill.go, which were held by siblings for this whole round and are free now.
- T-0125 item: ULID generation and dual-form ID validation, nothing switched over: ULID generation and dual-form validation live; nothing mints ULIDs yet. Implementer found a real bug while doing it: Num used fmt.Sscanf, which stops at the first non-digit, so a ULID id starting with digits would have silently returned a truncated number instead of 0 -- fixed with an all-digits check and a regression test. It also flagged that reItemHeading was still four-digits-only, outside its two-function lease: a ULID item would have been written to work.md and never parsed back, which would have lost every item the minter switch produced. Orchestrator closed that with a work.md round-trip test. IDRe and the heading regex now share the crockford alphabet constant with the generator, so grammar and generator cannot drift apart.
- P-0091 ULID item IDs behind the existing kind prefix, accepted alongside the legacy form: generation and acceptance delivered by T-0125 plus the heading-grammar fix. The minter switch remains open and is now unblocked.

## ITM-001
The item ID grammar SHALL accept both the legacy `<KIND>-NNNN` form and a `<KIND>-<26-char Crockford base32 ULID>` form, so records written before and after the switch both resolve.

Rationale: Every existing record, anchor and journal line carries the four-digit form; a grammar that rejected it would orphan the entire corpus. The kind prefix survives in both because a ULID carries order and uniqueness but not type.
