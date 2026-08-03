---
schema: v1
---

## B-01KYRQY892FSDSN75P9FFXFDM5 summary truncates on a raw byte slice and can emit invalid UTF-8, and status rides the prose composer untrusted on restore
kind: bug
state: active
created: 2026-07-30
grilled: 2026-08-03 open=0
targets: internal/lifecycle, internal/item

Two findings from the independent verification of B-01KYN3E973F20, both in internal/lifecycle, neither reachable through a public tool today, so both are latent rather than live. Recorded so they are not re-found.

FINDING 1, invalid UTF-8 from a naked slice. summary truncates with a raw s[:400] byte slice, so a multi-byte rune straddling the boundary leaves a dangling lead byte. Reproduced at pad=380 with a trailing em dash, yielding the byte tail e28094e2 - a broken final rune. This is exactly the defect capRetainedBodyTo has a RuneStart loop to prevent, in a sibling function that never got the same treatment. summary can also return a multi-line value, because gistLine returns an ADR Decision verbatim, so it has the same line-contract hazard as the truncation marker.

FIX: use the same rune-boundary walk, and flatten newlines, or better, have summary delegate to the existing capped helper instead of keeping a second truncation implementation. Two truncators in one package with different correctness properties is the underlying problem.

FINDING 2, SEVEN carried fields are restored raw while only three are coerced. carryRecord caps Status with capRetainedBodyTo, the composer for prose fields, although CheckHeader requires status to be a single line. restoreRecord coerces Context, Decision and Consequences through item.NormalizeHeaderValue but assigns everything else straight from the event. A verifier measured this at the EVENT level rather than by argument: Ti, Par, Gr, Tg with a newline and separately with a comma, Refs and Nd ALL restore to a record that CheckHeader then refuses - so seven carried fields, not the three that were fixed.

NOT reachable today: every write path pins Status to a hardcoded constant or to item.ValidStatus, and every other carried value originates from an already-stored item that the write guard has already vetted, so no public tool can put an illegal value on the event. The routes that remain are a v0-era journal written before the guard existed - migrate passes journal fields through byte-faithfully - or a hand-edited journal.

FIX: coerce every carried field on restore, not three of them, and stop routing an enum field through the prose composer at all. The reasoning that made the prose fields coerce rather than refuse applies unchanged to all seven: the restore path has no caller to report to, so refusing there strands the record instead of protecting it. Applying that reasoning to a subset is the actual defect here.

VERIFY. For finding 1, a test that summary output is valid UTF-8 and single-line for a value ending in a multi-byte rune at the boundary. For finding 2, a test that a journal event carrying an illegal value in ANY header field still restores to a writable record - the generalization of TestRestoreRecordAlwaysWritable, which today covers only the three prose fields. Table-drive it over all seven so the next field added cannot quietly skip coercion.

ALSO: the doc comment at lifecycle.go around line 752 cites adrOutcome, which no longer exists - carryRecord now caps per field. Stale reference, fix while in the file.

## B-01KYS6ZJQSE1E9MP7MQZB0YN1D archived is not terminal for any item that was ever rejected, and the refusal from archived misnames the reason
kind: bug
state: draft
created: 2026-07-30
targets: internal/lifecycle/lifecycle.go

Two findings, adversarially verified, both about the terminality of archived.

FINDING 1, HIGH, archived is resurrectable. lifecycle.Move resolves a work.md-absent ID by falling back to lastReject WITHOUT consulting Tombstone first. An archived item is also absent from work.md, so any archived item that carries an OLDER reject event resolves as state=rejected, and Allowed of rejected permits draft, submitted, approved and active. The item is pulled back out of the archive into work.md as a live record, exit 0, ordinary success line. The package doc states archived is terminal and the manifest states revocation may never reach done or archived; both are false in the presence of a prior rejection.

WORSE THAN A WRONG STATE: the archive tombstone and the spec.md intent line REMAIN, so the workspace is split-brained - the record is simultaneously live in work.md and archived in the journal, and find scope=history reports it archived while state reports it active. This is directly reachable through the documented revocable-rejection feature, which this session used three times to widen targets. Any record that was ever revocably rejected and later archived is affected.

FIX DIRECTION: Move must consult Tombstone BEFORE lastReject when the ID is absent from work.md, and refuse with a terminality message if the tombstone says archived. The ordering is the whole bug. A test must cover the specific sequence reject, revoke, archive, then attempt every move - which no current test does, because the two features were tested separately.

FINDING 2, MEDIUM, the refusal misnames itself. Every move out of a never-rejected archived item refuses with lifecycle: unknown item ID. The item is not unknown - get resolves it as an archived tombstone in the same session. So the refusal misstates the reason, never says archived is terminal, and leaves the caller unable to distinguish a typo from a correct ID on a terminal record. Per SRF-001 a refusal must lead with what did not happen; naming the wrong cause is worse than terse, because it sends the caller to check the ID rather than to accept terminality.

Separately and in the same family: every ARG E refusal prints the 26-character full ID while every success record and the GATE E path print the 13-character short form, so a caller copying an ID out of a refusal gets a different vocabulary than the rest of the surface. Pick one width for the whole surface.

VERIFY. reject, revoke to draft, archive, then move to active must refuse and the item must stay out of work.md; find scope=history and state must agree. Move from a never-rejected archived item must refuse naming terminality, not unknown item. Assert the ID width is identical across a success line, an ARG E refusal and a GATE E refusal.
