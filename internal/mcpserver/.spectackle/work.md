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

## T-01KYT2EHRMEAHSAY9GECXG2035 sweep the remaining hard-coded enumerations onto their source of truth
kind: task
state: draft
created: 2026-07-30
targets: internal/mcpserver/commands.go, internal/mcpserver/tools.go, internal/mcpserver/prompts.go, internal/mcpserver/server.go

Found by the mutation verifier of B-01KYS7111XFHZ after that record fixed THREE hard-coded copies of the blocked-exit enumeration - two it set out to fix, and a third the verifier measured on the failing-verdict route, which is arguably the commoner way into blocked. The class has now recurred often enough in this codebase to be worth sweeping deliberately rather than one site at a time.

WHY IT MATTERS, measured rather than argued: a hard-coded enumeration is not merely duplication, it goes WRONG. override-once is one-shot, so a second escalation offers only rescope and reject, and every hard-coded copy kept advertising override-once - which the parser then refused at exit 1. A refusal exists to teach the value that will be accepted; naming one that will not costs the caller a whole call to discover.

REMAINING INSTANCES, all in non-test code, all reported with locations by the verifier. commands.go around line 227 spells claude-pipe-copilot-pipe-codex-pipe-kimi while validHarnesses at commands.go:68 is the actual set. tools.go around 1495 spells U-pipe-E-pipe-S-pipe-N-pipe-O-pipe-C while ears.patternNames at internal/ears/ears.go:30 is the actual set. Those two are the same shape as the fixed bug and should render from their maps.

SEPARATE JUDGEMENT NEEDED, not a mechanical fix: the static doc surfaces at prompts.go:86, tools.go:285 and server.go:81 also name all three blocked outcomes. Those are TRUE in general - the full set does exist - so they are not wrong the way a refusal is. But they sit awkwardly against the HINT-001 reasoning used to strip the same enumeration from nextAction: values belong on the refusal that rejects a wrong one, not on a line every session pays for. Decide it once for all three rather than per-site, and record the decision; they are manifest and tool-description text, so any change there is measurable on the schema and manifest lines the benchmark now meters.

DIRECTION for the mechanical two: render from the map, exactly as blockedExitOutcomes now does for the escalation surfaces. Add a test per enumeration asserting the rendered string equals the map contents, so adding a harness or an EARS pattern cannot leave a stale advertisement behind.

VERIFY per RECMERGE-003: for each site, mutate the underlying map - add a value - and assert the suite fails because the rendered enumeration no longer matches. That is the property, not the literal.

## B-01KYTK0BGRF7X9PEY3QKWQ7TQA a rule added outside any record lifecycle has no edge to commit it, which invites bypassing the branch and PR flow
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/tools.go

OBSERVED on myself, which is the only reason it is worth filing: I added RECMERGE-004 with rule op=add while no record was active. The server wrote spec.md and made its own spectackle-paren-rule edge commit, and I then pushed that commit straight to main - bypassing the one-task one-branch one-PR flow this repository otherwise holds to. Nothing else rode along and main is clean, but the path was wrong and I reached for it because the alternative was unclear.

THE GAP. Every other write in this system is carried by a lifecycle edge: a move commits, pushes, opens or merges a PR, and the gates run. A standing rule is explicitly meant to be added the MOMENT a learning emerges - the workflow says so - which is frequently not while a record happens to be active. So the one write the process most wants to be immediate is the one write with no edge to carry it. The natural next step for an agent in that position is a manual git command, which is exactly the behavior the flow exists to prevent.

WHAT IS NOT THE FIX: telling agents to always have an active record first. That inverts the instruction to cast learnings immediately, and it would make a rule addition cost a full lifecycle - which is how learnings end up in agent-private notes instead, the failure the instruction is written against.

DIRECTION, and this needs a decision rather than a patch. Either a records-only write gets its own lightweight edge - commit, push, PR, gates - so the flow covers it, or rule op=add explicitly states that its write lands on the current branch and will be carried by the next edge, so the agent knows to leave it alone rather than improvise. The second is nearly free and may be sufficient; the first is more correct if rules should never sit uncommitted. Measure how long a pending rule typically waits for the next edge before choosing - if it is minutes, the cheap option wins.

VERIFY. Add a rule with no active record and confirm the surface tells the caller what happens to the write and what NOT to do about it. If the edge option is chosen instead, confirm the gates run on it and that a rule cannot reach main without them.
