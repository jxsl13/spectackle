---
schema: v1
---

## intent
- T-01KYB02KR0E81T1R7VXGKG0QD6 item.Refs: a general reference set with round-trip and existence validation: item.Refs live: general citation set, any kind to any kind, no lifecycle meaning. Emitted only when non-empty so every existing record renders byte-identically; Parse never rejects an unknown ref because a citation may point at an item archived out of work.md. Implementer added a selfID parameter to UnknownRefs -- correct, the two-arg form cannot detect self-citation. Wiring into draft/get and the grill question follows once tools.go is free.
- P-01KYB005M0FW0TFGBJ3Z4YG64W general item-to-item references, so deliberation chains exist and grill can demand them: deliberation chains shipped: item.Refs storage (T-01KYB02KR0E81T1R7VXGKG0QD6) plus surfacing (T-01KYC9QYQRF5JAF0N9S9QQGKD3); citations are now validated edges instead of body prose, and grill demands a weighing on proposals that record none
- B-01KYRN44EVEK2B0Q772MFEPZWK writeWork dedupes refs on untrimmed elements while the reader trims, so duplicates survive one write: validated pass by verifier-w1 diff 1cb6adc0b8e1
