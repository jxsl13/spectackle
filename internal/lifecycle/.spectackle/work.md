---
schema: v1
---

## B-01KYRQY892FSDSN75P9FFXFDM5 summary truncates on a raw byte slice and can emit invalid UTF-8, and status rides the prose composer untrusted on restore
kind: bug
state: draft
created: 2026-07-30
targets: internal/lifecycle/lifecycle.go

Two findings from the independent verification of B-01KYN3E973F20, both in internal/lifecycle, neither reachable through a public tool today, so both are latent rather than live. Recorded so they are not re-found.

FINDING 1, invalid UTF-8 from a naked slice. summary truncates with a raw s[:400] byte slice, so a multi-byte rune straddling the boundary leaves a dangling lead byte. Reproduced at pad=380 with a trailing em dash, yielding the byte tail e28094e2 - a broken final rune. This is exactly the defect capRetainedBodyTo has a RuneStart loop to prevent, in a sibling function that never got the same treatment. summary can also return a multi-line value, because gistLine returns an ADR Decision verbatim, so it has the same line-contract hazard as the marker.

FIX: use the same rune-boundary walk, and flatten newlines, or better, have summary delegate to the existing capped helper instead of keeping a second truncation implementation. Two truncators in one package with different correctness properties is the underlying problem.

FINDING 2, status carried through the prose composer but restored raw. carryRecord caps Status with capRetainedBodyTo, the composer for prose fields, although CheckHeader requires status to be a single line. restoreRecord was changed to normalize Context, Decision and Consequences but assigns Status raw - three of the four fields that share the composer. NOT reachable today: every write path pins Status either to a hardcoded constant or to item.ValidStatus, so a value long enough for the 2048 cap to fire, or holding a newline, cannot be stored in the first place. A verifier nonetheless reproduced the unrestorable-record outcome through Status and four other carried fields - title, targets, grilled, parent - by writing the journal event shape a pre-guard binary produces, which means a v0-era journal or a hand-edited one still reaches it.

FIX: normalize Status on restore like its three siblings, and stop routing an enum field through the prose composer at all. Then decide whether the other carried fields - title, grilled, targets, refs, needs, parent - should be coerced on restore too, on the same reasoning that made the prose fields coerce rather than refuse: the restore path has no caller to report to, so refusing there strands the record instead of protecting it. That reasoning applies to every carried field, and it was applied to only three.

VERIFY. For finding 1, a test that summary output is valid UTF-8 and single-line for a value ending in a multi-byte rune at the boundary. For finding 2, a test that a journal event carrying an illegal value in ANY header field still restores to a writable record - the generalization of TestRestoreRecordAlwaysWritable, which today covers only the three prose fields.

ALSO: the doc comment at lifecycle.go around line 752 cites adrOutcome, which no longer exists - carryRecord now caps per field. Stale reference, fix while in the file.
