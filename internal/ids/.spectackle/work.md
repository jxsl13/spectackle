---
schema: v1
---

## T-01KYF3BVBSFG6SWNXEQ5RKA28S raise MinRecordPrefixLen to 13: lifetime-stable short prefixes per the ADR-0013 amendment the user decided for v0.2.0
kind: task
state: draft
created: 2026-07-26
refs: ADR-01KYEP4Z5CFGBRNRX5AE59ZG2P
targets: internal/ids/ids.go, internal/ids/ids_test.go, docs

USER DECISION (ADR-01KYEP, answered 2026-07-26): raise the floor from 6 to 13. A 13-character prefix pins 5*13-2 = 63 bits - the full 48 timestamp bits plus 15 random bits - so two IDs share a 13-char prefix only on a same-millisecond mint with a 1-in-32768 tail collision: prefixes become stable for the repository lifetime instead of a ~17.5-minute window. The cost, accepted explicitly: every rendered record ID roughly doubles on the densest output surface.

WHAT TO CHANGE. internal/ids/ids.go MinRecordPrefixLen 6 -> 13 with the comment rewritten to the 63-bit arithmetic and the ADR citation; TestPrefixPinsFivePMinusTwoTimestampBits recalibrates automatically (it derives windowMs from the constant) - verify it still pins both sides at p=13 where the window is sub-millisecond, adjusting the boundary construction if the ms floor makes windowMs-1 degenerate; sweep tests that hardcode 6-char display prefixes; docs mentioning the floor or the 17.5-minute window (ids.go comment, any docs/ reference, the ADR-0013 text via its amendment note).

MIGRATION: existing journals and refs hold FULL IDs - only display shortening changes; no data migration. Anchors and stored prefixes: idScope resolution accepts any unambiguous prefix, so previously rendered 6-char prefixes in old records remain resolvable while new renders emit 13.

Bench: run the scripted A/B and record the per-lifecycle byte delta in the docs/bench-curves.md ledger - the cost is user-accepted, the measurement still gets recorded (TOKEN-OBJECTIVE-001).

VERIFY: go build ./... ; go test ./... -race ; the prefix-pinning test green at 13; bench A/B entry written.
ROLLBACK: revert the commit; rendered prefixes shrink back, resolution unaffected.
EXIT CRITERION: state and get render 13-char record prefixes; two IDs minted in different milliseconds never share a rendered prefix; the ledger carries the measured delta.
