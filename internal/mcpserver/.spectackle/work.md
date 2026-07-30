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
