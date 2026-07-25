---
schema: v0
---

## P-0083 the dev server always runs the current build: one command to rebuild and restart, and a hint when it drifts
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: Makefile, CONTRIBUTING.md

This repository develops itself with itself, so the resident server an agent drives IS the product under change. Every merged feature or fix makes the running binary older than the code that describes it, and the gap is invisible from the inside: tool output looks plausible because it comes from a real server, just not from the one in the tree.

Measured instances from this development cycle, both of which cost real time. A compact hint appeared broken and was investigated as a defect; the binary was 41 minutes older than its sources and the feature had in fact shipped. Separately, a resident server serving from a graph built at startup produced two false drift verdicts that auto-healed anchors with hashes for spans that were not the node — the same staleness family, one level up, and the reason DRF-003 now exists.

Two halves, because either alone leaves the hole open. Making the restart cheap is not enough if nobody notices they skipped it; detecting drift is not enough if fixing it is a five-command ritual nobody remembers.

Half one, a single command that rebuilds and restarts. The pieces already exist and are not yet composed: the Makefile builds, serve -http runs resident, and -pidfile (added this cycle) makes stopping a kill against a known file instead of a pgrep. Composing them must be idempotent and must never leave two servers bound to one port, since a half-dead second server is worse than a stale first one. Readiness has to be proven by an actual tool call rather than by a listening socket — the process binds before it finishes indexing, so a socket check would hand back a server that answers nothing.

Half two, the server notices for itself. It can compare its own executable's timestamp against the newest source file under its root and say so, exactly as the compact hint already nudges at a journal threshold — same debounced, once-per-crossing shape, so it informs without nagging. This turns an operator discipline into a property of the system, which is the whole argument for it: a rule nobody can forget beats a rule everybody agrees with.

Rejected: rebuilding automatically inside the server. A process that replaces its own binary mid-session would invalidate every in-flight lease and worktree, and a build failure would leave the agent with no server at all. Reporting is safe; self-surgery is not.

Rejected: a file watcher. It adds a dependency and a background goroutine to answer a question that a stat at tool-call time already answers, and the answer is only interesting when someone is actually using the server.

## T-0114 make dev: one idempotent command that rebuilds and restarts the resident server
kind: task
state: active
created: 2026-07-24
parent: P-0083
targets: Makefile, CONTRIBUTING.md, README.md

IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here.

GOAL
One command that leaves the resident server running the code currently in the tree. The pieces exist and are simply not composed: the Makefile builds, serve -http runs resident, and -pidfile makes stopping a kill against a known file. Compose them.

SCOPE (lease exactly these three)
  Makefile
  CONTRIBUTING.md
  README.md          the resident-service section only — add the make target as the recommended path; do not restructure
Do NOT touch internal/ or cmd/ at all. A sibling task owns internal/mcpserver right now, and a second sibling is adding the staleness hint inside the server — your half is purely the operator-facing command. .spectackle files are server-owned: never edit them by hand.

WHAT TO BUILD
A make target (name it dev) that, in order: builds the binary the way the existing build target does; stops a server already running for this workspace; starts a new one over Streamable HTTP with a pidfile; waits until it actually answers before returning.
Add companion targets for the two halves that are useful alone: stopping, and reporting whether one is running. Keep the names obvious.
Make the address and pidfile path overridable variables with sane defaults, following the file's existing style (GO, BIN, FUZZTIME, COVER_MIN are all overridable — match that).

THE THREE THINGS THAT MAKE OR BREAK IT
1. Idempotent, and never two servers on one port. Running the target twice in a row must leave exactly one server. A half-dead second process bound to nothing is worse than a stale first one. Stopping must succeed when nothing is running (fresh clone, first use) rather than failing the target.
2. Readiness must be proven by an actual tool call, not by a listening socket. The process binds the port before it finishes indexing, so a socket probe hands back a server that answers nothing. Use the binary's own call subcommand against the endpoint (spectackle call -http ADDR state) in a bounded retry loop, and fail the target with a clear message if it never answers — do not loop forever.
3. A stale pidfile must not wedge it. serve refuses to start when the pidfile already exists (O_EXCL, deliberate: an existing pidfile usually means a live server). If the recorded process is gone, the target must clear the file and proceed; if it is alive, it must stop it first. Read cmd/spectackle/main.go's pidfile handling before writing this — match its semantics rather than guessing them.

DOCUMENTATION
CONTRIBUTING.md: state the invariant plainly — this repository develops itself with itself, so the resident server IS the product under change, and it must be rebuilt and restarted after every merged feature or fix. Name the command. Give the reason rather than only the rule: a stale binary answers plausibly from code that no longer exists, which reads as a defect in the feature you just shipped. The `make all` section is the natural neighbor.
README: in the existing resident-service section, present the make target as the recommended way to start it, keeping the manual invocation for anyone not using make.

VERIFY (run every one; report real output, never predicted)
  make build
  make dev            twice in a row -- report both transcripts and prove exactly one server is running afterwards
  the stop target, then the status target, showing it reports not-running
  make dev with a stale pidfile present whose PID is dead -- must recover, not wedge
  make dev on a port already occupied by something else -- must fail with a clear message rather than hang
  make all            must still be green end to end
Paste the real output of each. A target that works only on the happy path is not done.

EXIT CRITERION
make dev is idempotent, proves readiness with a real tool call, recovers from a stale pidfile, fails loudly on an occupied port, make all is green, and both documents state the invariant with its reason.

ROLLBACK
Make targets and prose. Removing the targets and reverting the two documents restores the prior state; no Go code, schema, record or anchor is touched, and nothing else in the Makefile depends on the new targets.

REPORT BACK
The target names and variables, the real transcript of every verification above including the two failure cases, and anything you deliberately did NOT do.

## B-0001 replay rejects worktree journals inheriting pre-eid baseline events; suggested compact cannot clear them
kind: bug
state: active
created: 2026-07-25
targets: internal/replay/replay.go

DEFECT
replay.Run errors on ANY worktree journal event with empty Eid (replay.go:74) with the hint "compact on main first". The repo's root journal carries 7 pre-swarm archive events (2026-07-24, before Eid existed); every worktree inherits them at branch time, so every work op=submit fails. The hinted remedy cannot work: compact's journal fold keeps reject/archive/compact events verbatim (tools.go compact) and only runs past Compact.JournalMax — pre-eid archive events are permanent by design. Net effect: any workspace with pre-swarm archive history is permanently unable to submit worktrees. Reproduced on this repo with T-0114's verified worktree.

CAUSE
The eid guard fires before the baseline filter. A no-eid event inherited from the branch-point baseline is by definition already materialized on main and needs no replay; only a NON-baseline event without eid is a real invariant violation.

FIX (decision)
baselineEids additionally collects a legacy-key set (T+Ev+ID) for baseline events lacking eid; the delta loop skips a no-eid worktree event whose legacy key is in that baseline set and keeps the hard error for no-eid events NOT in the baseline (reworded, since compact cannot help). Both sides key through journal.Event's parsed time to avoid format drift. Rejected: backfilling eids during compact — changes archived history bytes, and existing worktrees still carry the pre-eid copies, so the error would persist until every open worktree is re-branched.

VERIFY
go test ./internal/replay/... -race with two new tests (baseline pre-eid event skipped; non-baseline pre-eid event still errors); go test ./...; live: T-0114 work op=submit succeeds after this fix.

ROLLBACK
One function's return set and one loop condition; reverting replay.go restores prior behavior. No schema, record or journal format change.
