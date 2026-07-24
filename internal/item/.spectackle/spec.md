---
schema: v0
---

## intent
- T-0109 item.Refs: a general reference set with round-trip and existence validation: item.Refs live: general citation set, any kind to any kind, no lifecycle meaning. Emitted only when non-empty so every existing record renders byte-identically; Parse never rejects an unknown ref because a citation may point at an item archived out of work.md. Implementer added a selfID parameter to UnknownRefs -- correct, the two-arg form cannot detect self-citation. Wiring into draft/get and the grill question follows once tools.go is free.
- P-0078 general item-to-item references, so deliberation chains exist and grill can demand them: storage layer delivered by T-0109 (item.Refs). Wiring Refs into draft/get and the grill question that demands a recorded weighing remain open -- they need tools.go and grill.go, which were held by siblings for this whole round and are free now.

## ITM-001
The item ID grammar SHALL accept both the legacy `<KIND>-NNNN` form and a `<KIND>-<26-char Crockford base32 ULID>` form, so records written before and after the switch both resolve.

Rationale: Every existing record, anchor and journal line carries the four-digit form; a grammar that rejected it would orphan the entire corpus. The kind prefix survives in both because a ULID carries order and uniqueness but not type.
