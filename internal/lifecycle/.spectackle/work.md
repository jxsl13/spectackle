---
schema: v1
---

## B-01KYRQY892FSDSN75P9FFXFDM5 summary truncates on a raw byte slice and can emit invalid UTF-8, and status rides the prose composer untrusted on restore
kind: bug
state: draft
created: 2026-07-30
targets: internal/lifecycle/lifecycle.go

Two findings from the independent verification of B-01KYN3E973F20, both in internal/lifecycle, neither reachable through a public tool today, so both are latent rather than live. Recorded so they are not re-found.

FINDING 1, invalid UTF-8 from a naked slice. summary truncates with a raw s[:400] byte slice, so a multi-byte rune straddling the boundary leaves a dangling lead byte. Reproduced at pad=380 with a trailing em dash, yielding the byte tail e28094e2 - a broken final rune. This is exactly the defect capRetainedBodyTo has a RuneStart loop to prevent, in a sibling function that never got the same treatment. summary can also return a multi-line value, because gistLine returns an ADR Decision verbatim, so it has the same line-contract hazard as the truncation marker.

FIX: use the same rune-boundary walk, and flatten newlines, or better, have summary delegate to the existing capped helper instead of keeping a second truncation implementation. Two truncators in one package with different correctness properties is the underlying problem.

FINDING 2, SEVEN carried fields are restored raw while only three are coerced. carryRecord caps Status with capRetainedBodyTo, the composer for prose fields, although CheckHeader requires status to be a single line. restoreRecord coerces Context, Decision and Consequences through item.NormalizeHeaderValue but assigns everything else straight from the event. A verifier measured this at the EVENT level rather than by argument: Ti, Par, Gr, Tg with a newline and separately with a comma, Refs and Nd ALL restore to a record that CheckHeader then refuses - so seven carried fields, not the three that were fixed.

NOT reachable today: every write path pins Status to a hardcoded constant or to item.ValidStatus, and every other carried value originates from an already-stored item that the write guard has already vetted, so no public tool can put an illegal value on the event. The routes that remain are a v0-era journal written before the guard existed - migrate passes journal fields through byte-faithfully - or a hand-edited journal.

FIX: coerce every carried field on restore, not three of them, and stop routing an enum field through the prose composer at all. The reasoning that made the prose fields coerce rather than refuse applies unchanged to all seven: the restore path has no caller to report to, so refusing there strands the record instead of protecting it. Applying that reasoning to a subset is the actual defect here.

VERIFY. For finding 1, a test that summary output is valid UTF-8 and single-line for a value ending in a multi-byte rune at the boundary. For finding 2, a test that a journal event carrying an illegal value in ANY header field still restores to a writable record - the generalization of TestRestoreRecordAlwaysWritable, which today covers only the three prose fields. Table-drive it over all seven so the next field added cannot quietly skip coercion.

ALSO: the doc comment at lifecycle.go around line 752 cites adrOutcome, which no longer exists - carryRecord now caps per field. Stale reference, fix while in the file.

## B-01KYS1028RE8NTN07F6HSGBRPH capGist doc comment is attached to the wrong symbol, its ordering contract is unpinned, and its comment overstates the coverage
kind: bug
state: active
created: 2026-07-30
targets: internal/lifecycle/lifecycle.go, internal/lifecycle/lifecycle_test.go

Three defects in the change that closed B-01KYRQXJ99F48, all found by the independent verifier of that record AFTER it had archived - the verdict could not be recorded because verdicts bind to live items, so the findings arrived with nowhere to land. Filed here rather than lost.

DEFECT 1, doc comment attached to the wrong symbol. var gistLineEndings was inserted between capGist doc comment and capGist itself with no blank line, so go doc -u ./internal/lifecycle prints the whole capGist bounds a one-line digest paragraph as documentation for gistLineEndings, and capGist is left with no doc comment at all. gofmt and go vet are both clean, so nothing catches it.

DEFECT 2, a stated contract with nothing pinning it. The comment asserts CRLF-first order so a CRLF becomes one space rather than two. Reordering the replacer to put the LF rule first leaves the ENTIRE suite green; the measured effect is capGist of a-CRLF-b returning two spaces instead of one. This is the same shape the repository has already been bitten by twice - a behavioral guarantee that lives only in prose - and it is the one place the B-01KYRQXJ99F48 change repeats the pattern it was written to fix, which is why it is worth fixing rather than shrugging at. Consequence on its own is small: doubled spaces and slightly less content under the cap.

DEFECT 3, the comment overstates its coverage. It claims every line ending a caller can supply. Measured false: U+2028 LINE SEPARATOR, U+2029 PARAGRAPH SEPARATOR, U+0085 NEL, VT and FF all survive capGist. Against the contract the fix actually named - CommonMark, which defines a line ending as LF, CR or CRLF only - the fix IS complete, so the code is right and the sentence is wrong. A conforming UAX#14 viewer could still break such a bullet, but it cannot become debris: the marker leading space means no separator is ever directly adjacent to it, the record ID still leads the line, and Go reads one line so the dedupe keys it.

FIX. Move the var above capGist doc comment with a blank line between them so each symbol documents itself. Add a test that pins the CRLF-to-one-space behavior by exact string equality, not by a contains check. Narrow the comment to name CommonMark and state explicitly which separators are deliberately NOT touched and why, so the next reader does not widen the set without a measurement.

VERIFY. go doc -u ./internal/lifecycle shows a doc comment on capGist and a separate one on gistLineEndings. Reordering the replacer fails the new test. The new test asserts capGist of a-CRLF-b equals a-space-b exactly, plus the lone CR and lone LF cases and a CRLF run.
