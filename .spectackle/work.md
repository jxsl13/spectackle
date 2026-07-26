---
schema: v1
---

## ADR-01KYFYGVSRFX4B9B2YJ44QSBS8 live probe: should the widget cache be bounded
kind: adr
state: done
created: 2026-07-26
decision: yes bounded
status: accepted

kind: radio
option: yes bounded
option: no unbounded
choice: yes bounded

## T-01KYGCJ6P6FFHB9VJK5S4359V8 cut v0.2.0: release notes from the archive intent log, ldflags version stamp, tag, GitHub release
kind: task
state: draft
created: 2026-07-26
targets: cmd/spectackle/main.go, internal/mcpserver/server.go, Makefile, docs/lifecycle.md

IMPLEMENTER IN OWN WORKTREE. The full review chain the user gated this release on is complete and live (grill verdicts, validation gates, edge commits, never-squash, one-task-one-PR, elicitation, lens labels, risk gating, tripwire, compaction survival, atomic archive edge, short IDs, role boundary, outcome benchmarks). Timing is the users call via the linked decide.

WHAT TO BUILD
1. RELEASE NOTES, derived not written: a small generator (make releasenotes or a script) that renders the v0.2.0 section from the ARCHIVE tombstones since the v0.1 line - the journal archive events summaries ARE the change log (the note is the training signal; here it is also the release note). Group by kind (features=task, fixes=bug, decisions=adr), short display IDs, one line each from the tombstone summary head. Human tops it with a two-sentence intro; the generated body is committed as docs/releases/v0.2.0.md.
2. VERSION STAMP: Version already ldflags-injectable (internal/mcpserver.Version, stamped via -X). Makefile gains a release target injecting the git tag; verify spectackle --version or the handshake renders v0.2.0 when built so.
3. TAG + RELEASE: tag v0.2.0 on the release commit (annotated, message = the two-sentence intro), gh release create v0.2.0 with the notes file. Tag AND release happen only after the notes PR merged - the tag is the LAST mechanical step and per the merge policy runs on the users standing instruction.
4. docs/lifecycle.md or README: version line updated if any names v0.1.
NON-NEGOTIABLE, tested where testable: the notes generator is deterministic over a fixture journal (golden); the version stamp renders in the handshake when injected (existing Version machinery, assert the ldflags path in a build test or the Makefile smoke); no hand-written change log drift - the generator output is the file, human intro excepted.
VERIFY: go build/test/vet/gofmt; lint; check ok; make releasenotes output pasted; a stamped build printing v0.2.0 pasted.
SCOPE: generator + Makefile target + notes file + tag/release mechanics. No server behavior changes.
ROLLBACK: delete tag and release; revert the notes commit.
REPORT: the generated notes verbatim, the stamped-version proof, the tag/release URLs.

## ADR-01KYGCJ70JESXTEGZJVWF7AXN4 v0.2.0: the full chain landed early - cut the release now or hold to the planned Aug 1-2 window?
kind: adr
state: submitted
created: 2026-07-26
status: proposed

kind: radio
option: cut now
option: hold to Aug 1-2
