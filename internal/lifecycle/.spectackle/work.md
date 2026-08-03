---
schema: v1
---

## B-01KYS6ZJQSE1E9MP7MQZB0YN1D archived is not terminal for any item that was ever rejected, and the refusal from archived misnames the reason
kind: bug
state: draft
created: 2026-07-30
targets: internal/lifecycle, internal/mcpserver

Two findings, adversarially verified, both about the terminality of archived.

FINDING 1, HIGH, archived is resurrectable. lifecycle.Move resolves a work.md-absent ID by falling back to lastReject WITHOUT consulting Tombstone first. An archived item is also absent from work.md, so any archived item that carries an OLDER reject event resolves as state=rejected, and Allowed of rejected permits draft, submitted, approved and active. The item is pulled back out of the archive into work.md as a live record, exit 0, ordinary success line. The package doc states archived is terminal and the manifest states revocation may never reach done or archived; both are false in the presence of a prior rejection.

WORSE THAN A WRONG STATE: the archive tombstone and the spec.md intent line REMAIN, so the workspace is split-brained - the record is simultaneously live in work.md and archived in the journal, and find scope=history reports it archived while state reports it active. This is directly reachable through the documented revocable-rejection feature, which this session used three times to widen targets. Any record that was ever revocably rejected and later archived is affected.

FIX DIRECTION: Move must consult Tombstone BEFORE lastReject when the ID is absent from work.md, and refuse with a terminality message if the tombstone says archived. The ordering is the whole bug. A test must cover the specific sequence reject, revoke, archive, then attempt every move - which no current test does, because the two features were tested separately.

FINDING 2, MEDIUM, the refusal misnames itself. Every move out of a never-rejected archived item refuses with lifecycle: unknown item ID. The item is not unknown - get resolves it as an archived tombstone in the same session. So the refusal misstates the reason, never says archived is terminal, and leaves the caller unable to distinguish a typo from a correct ID on a terminal record. Per SRF-001 a refusal must lead with what did not happen; naming the wrong cause is worse than terse, because it sends the caller to check the ID rather than to accept terminality.

Separately and in the same family: every ARG E refusal prints the 26-character full ID while every success record and the GATE E path print the 13-character short form, so a caller copying an ID out of a refusal gets a different vocabulary than the rest of the surface. Pick one width for the whole surface.

VERIFY. reject, revoke to draft, archive, then move to active must refuse and the item must stay out of work.md; find scope=history and state must agree. Move from a never-rejected archived item must refuse naming terminality, not unknown item. Assert the ID width is identical across a success line, an ARG E refusal and a GATE E refusal.
