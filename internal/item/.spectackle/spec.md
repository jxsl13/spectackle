---
schema: v0
---

## intent
- T-0109 item.Refs: a general reference set with round-trip and existence validation: item.Refs live: general citation set, any kind to any kind, no lifecycle meaning. Emitted only when non-empty so every existing record renders byte-identically; Parse never rejects an unknown ref because a citation may point at an item archived out of work.md. Implementer added a selfID parameter to UnknownRefs -- correct, the two-arg form cannot detect self-citation. Wiring into draft/get and the grill question follows once tools.go is free.
- P-0078 general item-to-item references, so deliberation chains exist and grill can demand them: deliberation chains shipped: item.Refs storage (T-0109) plus surfacing (T-0117); citations are now validated edges instead of body prose, and grill demands a weighing on proposals that record none
