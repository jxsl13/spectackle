---
schema: v1
---

## B-01KYRN43FQFZ4RCB2F1K0QBB9R knowledge apply exits 0 and prints ok applied beside a per-entry refusal
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/knowledge.go

Found by independent verification of B-01KYN3E973F20, confirmed pre-existing (identical on the pre-fix binary), so it is a separate defect rather than a regression.

OBSERVED. A knowledge apply whose entry is refused prints the refusal AND a success line, and exits 0:

  ! ARG E - apply adr <key>: <reason>
  ok applied added=0 gaps=1
  (exit 0)

EXPECTED per SRF-001: a refused operation exits non-zero and leads with what did NOT happen, never rendering a success-shaped line for a state the caller did not request. draft, decide and move all get this right - they exit 1 with no record line. knowledge apply is the outlier.

WHY IT MATTERS. An agent that checks the exit code sees success; an agent that reads the last line sees ok applied. Either way the refusal is invisible, so an import silently drops entries and the caller believes the artifact landed. added=0 is the only signal, and it is easy to read as nothing to do rather than something was rejected.

SCOPE NOTE. Deliberately not folded into B-01KYN3E973F20 even though that work already had knowledge.go in its targets: the exit-code contract is unrelated to header round-tripping, and mixing them would have made the archive note describe two unrelated changes.

FIX DIRECTION. Per-entry refusals need to reach the exit status. Decide whether a partially-applied artifact is a failure (any refusal exits non-zero) or a partial success (exit non-zero only when nothing applied), then make the render say which entries were refused and why, rather than reporting only a count. VERIFY: a test asserting exit status and absence of an ok-shaped line for an artifact with one refused entry and one applicable entry.

## B-01KYRVXQ02FDH9YBAFG64SH13N knowledge export mixes the artifact and its ok line on one stream, so export piped into apply fails with an unmappable line number
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/knowledge.go

Found by an independent verifier while checking something else, and it cost that verifier a wrong hypothesis before it identified the cause - which is the real damage here.

OBSERVED. knowledge op=export writes the artifact to stdout AND appends its own ok export record line to the same stream. Piping that stdout into op=apply body= therefore feeds the record line to the YAML parser, which fails with: yaml: line 17: could not find expected colon. The line number is ENTRY-RELATIVE, so it points at a coordinate the caller cannot locate in what it sent - the caller counts lines in its own input and finds nothing wrong at line 17.

EXPECTED. Either the artifact is the only thing on stdout so the obvious pipe works, or the refusal explains that the input carries a trailing record line and names it. The documented path= route works in both directions, so this is not a broken feature - it is a shape that invites a wrong call and then misdirects the caller who makes it.

WHY IT MATTERS FOR TOKEN ECONOMY. An agent that pipes export into apply gets a parser error naming a line it cannot find, and the cheapest recovery is to re-read both artifacts and the tool docs. The error is worse than no error, because it sends the reader to the wrong place. This is the same failure class as a refusal that renders a success-shaped line: the output describes a world the caller is not in.

FIX DIRECTION. Decide which stream owns the record line. Cleanest is that an op whose output IS a document emits only the document on stdout and its ok line on stderr, matching how a caller would naturally compose it. If the two must share a stream, the apply parser should detect a trailing record line and say so by name instead of surfacing a raw YAML error, and any line number it reports must be in the coordinate system of the input the caller supplied.

VERIFY. A test that pipes export output directly into apply and asserts it either succeeds or refuses with a message naming the trailing record line; plus an assertion that any reported line number resolves against the caller input.

## B-01KYS6Y5NKF42BA6P3RH1CMP6F the validation gate binds every verdict to an absent diff: itemDiff greps the full record ID while code commits carry only the short display prefix
kind: bug
state: active
created: 2026-07-30
rounds: 1
targets: internal/mcpserver/validate.go, internal/mcpserver/gitflow.go, internal/mcpserver/validate_test.go

Found by four independent agents on separate angles, adversarially refuted and confirmed, then reproduced end to end. This is a validity gap, not a policy question: feedback.validate=require is nominally enforced while the evidence it binds to is empty.

ROOT CAUSE, one line. itemDiff attribution fallback runs git log --grep with the FULL 26-character record ID and then drops every subject starting with spectackle-open-paren. The only commits whose message contains the full ID are the records and edge commits, which carry it in a Spectackle-Item trailer - and those are exactly the ones the filter discards. The three production code-checkpoint writers in gitflow.go emit spectackle plus shortDisplayID plus subject, i.e. the 13-character prefix and no trailer, so no gitflow code commit is ever attributed. Corrected by the refuter: swarm.go does commit with the full ID, and legacy short-shaped IDs like T-0135 are identity under shortDisplayID, so the claim is not universal - but it holds for every item whose code lands through the gitflow writers, which is the primary flow.

CONSEQUENCE CHAIN, all measured. itemDiff returns empty, diffHash of empty is the literal absent, and validateHash degenerates to sha256 of absent plus the NUL-joined target list - a pure function of the declared targets. Therefore: the staleness predicate in validateGateGap compares that constant to itself, so a verdict never expires no matter what the code does afterward; validateComputed classes are all functions of the diff, so the pack renders open=0 and addressalGap has nothing to demand, meaning a bare pass clears; validateRisk returns empty when the diff is empty, so the risk-gated require escalation can never trip. The apparatus does not degrade partially - it goes silent all at once, precisely in the case where there is no evidence at all. Reproduced: recorded a passing verdict, then committed a post-verdict change that gutted the implementation, re-rendered - still open=0 and the pass standing - and archived successfully.

IMPORTANT: the gate is NOT a some-pass-exists check. validateGateGap does compare v.Hash against validateHash and does honor the stale marker the pack renders. The defect is strictly upstream of a correct comparison, which is why it was invisible.

WHY THE SUITE NEVER CAUGHT IT. TestValidateDiffBinding asserts exactly the behavior that is broken in production, and passes, because its helper commits with the FULL ID and says so in its own comment. The fixture encodes the pre-shortDisplayID commit-subject format; the server later switched human-facing git surfaces to the short form and nothing re-pointed either the attribution grep or the fixture. Nothing in the suite exercises the subject format the server actually writes. That is the more important lesson than the grep itself.

CONFIRMED ON LIVE RECORDS. Every validate hash recorded for B-01KYN3E973F20 and B-01KYRQXJ99F48 equals sha256 of absent plus their target list, for each successive target set. For the latter, a real code commit carrying three post-verdict follow-ups landed between the passing verdict and the archive and did not move the hash. Both PR merges landed AFTER the archive, so merge-mode attribution never ran while the item was still gateable, and once archived no fresh verdict can be recorded at all.

FIX, option A of four considered. Grep shortDisplayID of the id instead of the id in citing(). The short form is a strict PREFIX of the full form, so a single grep matches both naming eras and the spectackle-open-paren subject filter still excludes records commits. Cheapest, and it restores the contract that was designed rather than inventing a new one. Accepted cost, measured: post-verdict code commits now expire verdicts, which fired on 1 of 66 archives in the probe. REJECTED for now: option B, hashing a canonical git diff base-to-tip so merge and pre-merge attribution agree - correct but needs a stable base ref per item; option C, making the absent-diff case a hard refusal under require rather than a silent pass; option D, leaving it and documenting. B remains worth doing because the two attribution modes currently hash the same tree differently, so any fix that revives fallback makes the PR merge itself a staleness event.

ALSO FIX. The durable archive note composes validated pass by AGENT diff HASH, and that string becomes the tombstone and the spec.md intent line. When the verdict bound an absent diff, that identifier is the hash of the literal string absent - the note reads as a commitment to reviewed code and is a commitment to target NAMES. The pack is honest about this and says d absent, verdict proceeds on pack-absent evidence; the archive note is not. Either carry the absence into the note or do not print a diff identifier at all.

VERIFY. A test that commits through the REAL writer path rather than a hand-rolled helper - assert the subject format gitflow actually produces is attributable - plus a test that a post-verdict code commit moves validateHash and that the gate then refuses under require. Both must fail against the current code.
