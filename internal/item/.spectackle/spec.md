---
schema: v0
---

## intent
- T-0109 item.Refs: a general reference set with round-trip and existence validation: item.Refs live: general citation set, any kind to any kind, no lifecycle meaning. Emitted only when non-empty so every existing record renders byte-identically; Parse never rejects an unknown ref because a citation may point at an item archived out of work.md. Implementer added a selfID parameter to UnknownRefs -- correct, the two-arg form cannot detect self-citation. Wiring into draft/get and the grill question follows once tools.go is free.
