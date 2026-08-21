---
schema: v1
---

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
