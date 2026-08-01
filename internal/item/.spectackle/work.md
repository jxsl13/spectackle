---
schema: v1
---

## B-01KYRN44EVEK2B0Q772MFEPZWK writeWork dedupes refs on untrimmed elements while the reader trims, so duplicates survive one write
kind: bug
state: done
created: 2026-07-30
targets: internal/item/item.go

Found by independent verification of B-01KYN3E973F20 (6 hits in 19908 randomized accepted cases).

OBSERVED. Refs is documented as duplicate-free, and writeWork calls dedupeStrings before joining. dedupeStrings compares the elements as given, but splitList trims each element on read, so refs [" refs","refs "] are two distinct strings at write time and the same string at read time: the file holds both and the loaded item carries the duplicate. It self-heals on the next write, because by then both elements are already trimmed and compare equal.

EXPECTED. The documented invariant holds on the first write, not eventually.

WHY IT MATTERS. Small, but it is the same shape as the bug that motivated the round-trip rule: writer and reader disagree about what a value IS. Anything that counts refs or joins on them sees a duplicate for exactly one generation.

RELATED. A direct consequence of the deliberate decision in B-01KYN3E973F20 to tolerate whitespace around list elements rather than refuse it - that decision was justified on the grounds that trimming reaches a fixed point rather than drifting, which verification confirmed, but the fixed point is reached one write LATER than the dedupe.

FIX DIRECTION. Trim before dedupe in writeWork so both sides agree, i.e. normalize each element then dedupe. Also decide whether empty, whitespace-only and bare - elements should be refused rather than silently dropped by splitList (currently silent). VERIFY: a test writing [" a","a ","a"] and asserting the file holds one element and the reloaded item one ref, plus the empty/dash cases.
