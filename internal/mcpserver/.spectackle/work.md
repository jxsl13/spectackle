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
