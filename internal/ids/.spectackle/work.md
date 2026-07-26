---
schema: v1
---

## ADR-01KYEP4Z5CFGBRNRX5AE59ZG2P MinRecordPrefixLen=6 pins only 28 timestamp bits (~17.5 min window): IDs minted in the same session but >17.5 min apart can share a 6-char prefix, so short prefixes are stable only within that window. Raise the floor to ~13 chars (pins 63 bits, prefix stable for the repo lifetime) at the cost of much longer visible IDs, or keep 6 and accept occasional re-disambiguation? This amends ADR-0013.
kind: adr
state: submitted
created: 2026-07-26
status: proposed

kind: radio
option: keep 6 (short IDs, window-local stability)
option: raise to 13 (lifetime-stable prefixes, long IDs)
blocks: B-01KYD4J254FK5BE486GKFNMN39
