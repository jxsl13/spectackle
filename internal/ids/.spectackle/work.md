---
schema: v1
---

## B-01KYD4J254FK5BE486GKFNMN39 MinRecordPrefixLen's rationale miscounts the encoding, so the six-character floor pins 28 timestamp bits, not 30
kind: bug
state: active
created: 2026-07-25

DEFECT
The doc comment on ids.MinRecordPrefixLen argues the floor from arithmetic that is off by the encoding's two slack bits. It states that a prefix of p characters pins 5p of the leading 48 timestamp bits, so p=6 pins 30 bits, i.e. the mint time to within 2^18 ms, about 4.4 minutes. The encoding is a fixed-width big-endian base32 numeral over the whole 128-bit value, and RecordIDLen's own comment records the consequence: the first character carries only 3 bits, the other two being leading zeroes. So p characters pin 5p-2 bits, p=6 pins 28, and the window is 2^20 ms - about 17.5 minutes, four times the stated figure.

MEASURED
Probe over ids.MintRecordIDAt with a fixed base timestamp: a six-character prefix taken from the first record fails to resolve against a second record minted 0 ms, 1 ms, 1 s, 1 min AND 10 min later - every one of them an ambiguity naming both records. Ten minutes is inside the real 17.5-minute window while sitting outside the documented 4.4-minute one, which is the discrepancy made visible.

WHY IT IS MORE THAN A COMMENT BUG
The comment is the stated justification for the constant, and it makes the floor look like it buys a useful degree of uniqueness. It buys none: with a ~17.5-minute bucket, essentially every pair of records in one working session collides at six characters, so ShortenRecordID's adaptive path decides the length in practice and the floor only ever states identity, never uniqueness. That in turn is why a displayed ID captured early in a session goes ambiguous a few drafts later - correct behavior for a git-style prefix, but a sharp edge for an agent copying IDs between calls, and one nothing in the code or docs currently warns about. Observed live: two proposals drafted a second apart, the first rendered P-01KYD4 and the second P-01KYD4CDRB; get on the first, six-character form then refused as ambiguous.

FIX (decision at implementation)
Correct the arithmetic in the comment either way. Then decide whether the floor is still the right constant now that its stated benefit is four times smaller than claimed: raising it to about 13 characters would make a displayed ID stable against same-millisecond siblings and mostly stable in general, at a token cost between the six-character floor and ADR-01KYCZ13KRF84VD5DSVQ4017MV's measured 11 percent for full IDs. That is an ADR-01KYCZ13KRF84VD5DSVQ4017MV amendment, not a code cleanup, so it belongs in a decision rather than in this bug.

VERIFY
A test asserting the bit arithmetic the comment claims - the number of timestamp bits a p-character prefix pins - so the two cannot drift apart again.

ROLLBACK
Comment-only if the constant is left alone.
