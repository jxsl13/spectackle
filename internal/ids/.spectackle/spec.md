---
schema: v1
---

## intent
- B-01KYD4J254FK5BE486GKFNMN39 MinRecordPrefixLen's rationale miscounts the encoding, so the six-character floor pins 28 timestamp bits, not 30: DEFECT
- ADR-01KYEP4Z5CFGBRNRX5AE59ZG2P MinRecordPrefixLen=6 pins only 28 timestamp bits (~17.5 min window): IDs minted in the same session but >17.5 min apart can share a 6-char prefix, so short prefixes are stable only within that window. Raise the floor to ~13 chars (pins 63 bits, prefix stable for the repo lifetime) at the cost of much longer visible IDs, or keep 6 and accept occasional re-disambiguation? This amends ADR-0013.: kind: radio
