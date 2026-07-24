---
schema: v0
---

## ADR-0012 How should spectackle coordinate agents beyond a single host: keep the central SQLite counter, or remove it in favor of coordination-free identifiers?
kind: adr
state: done
created: 2026-07-24
context: Verified current state: one coord.db at <main>/.spectackle/cache/coord.db, opened by every process via git rev-parse --git-common-dir, WAL mode, and NextID mints from a counters table with a file-scan floor so a deleted db cannot regress ids. The design is sound single-host; nothing about swarm is broken. Durable records already distribute through git (work.md, journal.ndjson, spec.md); coord.db is explicitly ephemeral, holding leases, heartbeats, the event log and the counters. Only the counters genuinely require a single serialization point. Correction to a premise raised during the discussion: plain SQLite over a network mount does NOT provide single-host guarantees, because its locking depends on filesystem advisory locks that many NFS implementations implement incorrectly; upstream warns against multi-host use. Distributed guarantees require a SQLite-compatible replicated system (rqlite, LiteFS, Turso/libSQL) or a different database. dqlite is excluded by the standing pure-Go constraint. coord already speaks database/sql, so a driver/DSN swap is far cheaper than an interface abstraction.
decision: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable
consequences: Deciding constraints named by the user: pure Go, no cgo (excludes dqlite and any C-linked backend), and lease correctness under partition (a lease two hosts both believe they hold is worse than no lease, so TTLs become load-bearing and an eventually-consistent store is not acceptable for leases). UUIDv7 gives creation-ordered, lexicographically sortable ids without any coordination, which is what removes the counter as the single serialization point. Unresolved and load-bearing: every record, doc, test and the token-economy argument assume short sequential ids like P-0084 and MCP-013; a 36-character id in every record line is a direct cost to the output diet this project treats as a feature. The proposed resolution is a git-style short rendering -- UUIDv7 as the canonical identity, a short prefix as the displayed form, lengthened locally on collision -- which keeps both properties, but it was not among the options offered and needs confirmation before implementation. Leases still need a correct store under partition; UUIDv7 solves identity, not mutual exclusion, so that half remains open.
status: accepted

kind: radio
option: keep the central SQLite counter and stay single-host
option: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable
option: abstract coord behind a Go interface with multiple backends
blocks: P-0088
choice: remove the central counter: UUIDv7 identifiers, coordination-free and lexicographically sortable

## P-0092 generate a pre-commit hook that keeps record files off worktree branches, and harden make dev
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24
targets: internal/mcpserver/commands.go, Makefile

Two failures from the same root, both observed in this repository rather than imagined.

An orchestrator shell kept a working directory inside a linked worktree after harvesting it. The next commit therefore landed on the worktree branch instead of the development branch, carrying .spectackle record files with it, and the same slip started a resident server whose pidfile was written inside that worktree — so when the worktree was deleted the process survived while its pidfile vanished, and make dev then failed to bind the port with no way to recover.

A hook catches the first. Git reports a linked worktree unambiguously: rev-parse's git-dir and git-common-dir differ exactly there. Hooks live in the common directory, so one installation covers every worktree of a repository.

The rule has to be narrower than refusing worktree commits outright, because the sanctioned worktree flow commits code there — work op=submit depends on it. SPX-SWM-001 already draws the line: agent branch commits exclude .spectackle paths. So the hook rejects exactly the commits that cross it, and the legitimate path stays open.

Two honest limits, worth stating rather than discovering: hooks are not versioned, so installing one is a local act a fresh clone does not inherit, and --no-verify bypasses it. This is a guardrail, not a guarantee.

The second failure is a gap in make dev's own recovery. It handles a stale pidfile whose process is dead; it does not handle the inverse, a live process holding the port with no pidfile at all. That case is not exotic — it is what a deleted worktree leaves behind — and it currently ends in a bind error with no path forward except finding the process by hand.

Rejected: making the hook refuse every commit inside a linked worktree. It would break work op=submit, which is the entire point of the worktree flow.

Rejected: solving the pidfile case by having the server write to an absolute path derived from the main repo. It would work, but it silently couples a general-purpose flag to this repository's worktree convention; recovering in the target that owns the lifecycle is the smaller commitment.

## T-0126 generated pre-commit hook keeps records off worktree branches; make dev recovers a lost pidfile
kind: task
state: active
created: 2026-07-24
parent: P-0092
targets: internal/mcpserver/commands.go, internal/mcpserver/commands_test.go, internal/mcpserver/templates/hooks/pre-commit.tmpl, Makefile

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Read internal/mcpserver/commands.go (the generator is data-driven; you are extending it) and the Makefile's dev/dev-stop/dev-status targets.

TWO INDEPENDENT FIXES. Do them in either order.

SCOPE (lease exactly these four)
  internal/mcpserver/commands.go                        hook generation
  internal/mcpserver/commands_test.go
  internal/mcpserver/templates/hooks/pre-commit.tmpl    NEW
  Makefile                                              dev-stop recovery only
Do NOT touch internal/mcpserver/tools.go, grill.go, swarm.go, decide.go, knowledge.go, internal/item (a sibling task owns it right now), internal/coord, internal/journal, internal/knowledge, cmd/, README.md, docs/. .spectackle files are server-owned: never edit them by hand.

FIX 1 — the pre-commit hook
Contract MCP-015 (read it with get id=MCP-015): WHEN a commit inside a linked git worktree stages any `.spectackle` path, the generated pre-commit hook SHALL exit 1 naming every offending path, while leaving code-only worktree commits untouched.
Detection of a linked worktree: `git rev-parse --git-dir` and `git rev-parse --git-common-dir` resolve to DIFFERENT paths exactly there, and to the same path in the main worktree. Compare them after resolving to absolute paths; do not pattern-match directory names.
Staged paths: `git diff --cached --name-only`. Reject if any staged path contains a `.spectackle/` component. Print every offender, not just the first — a partial list makes the fix take two attempts.
Do NOT reject code-only commits in a worktree. The sanctioned flow (work op=submit) commits code there, and breaking it would break the entire worktree workflow. A test must prove this case passes.
Hooks live in the repository's COMMON git directory, so one installation covers every worktree. Write it to the path `git rev-parse --git-common-dir` reports, under hooks/pre-commit, and make it executable (0o755 — a non-executable hook is silently ignored by git, which is the worst failure mode here).
Generation: add it to the existing commands tool as a new op (name it install-hooks or similar) rather than a new tool — the tool count stays minimal. It must be idempotent, and it must NOT clobber an existing pre-commit hook it did not write: if a file exists without the generated marker, refuse and say so. Use the same do-not-edit header convention the command templates use, so the marker is the header itself.
Write the hook as POSIX sh, not bash: it runs on whatever the user has.

FIX 2 — make dev recovers a live process with no pidfile
dev-stop currently handles a stale pidfile whose process is dead. It does not handle the inverse: no pidfile at all while a live process holds the address. That is what a deleted worktree leaves behind — the server wrote its pidfile inside the worktree — and today it ends in `bind: address already in use` with no path forward.
Make dev-stop, when no pidfile exists, detect a listener on DEV_ADDR and stop it before dev proceeds. Use only tools that are reasonably present (ss, lsof or fuser — probe for what exists and degrade with a clear message rather than failing obscurely if none is available). Report clearly what it is stopping and why, since killing a process the user did not start deserves a visible line.
Do not widen this into general process management. The target owns DEV_ADDR; that is the whole warrant.

TESTS
  commands_test.go:
    1. the hook template renders, is non-empty, and carries the do-not-edit header.
    2. installing writes the file to the common-dir hooks path with mode 0o755.
    3. installing twice is idempotent.
    4. an existing pre-commit hook WITHOUT the generated marker is refused, not overwritten, and the original bytes survive.
  For the hook's own behavior, drive the script directly: create a temp repo, add a linked worktree, stage a .spectackle path in the worktree and assert the script exits non-zero naming it; stage only a code file and assert it exits 0; stage a .spectackle path in the MAIN worktree and assert it exits 0 (the rule is about worktree branches, not about the main checkout).

VERIFY (run every one; report real output, never predicted)
  go build ./...
  go test ./internal/mcpserver/... -race
  go test ./...
  go vet ./internal/mcpserver/...
  /home/user/spectackle/bin/spectackle lint
  make dev twice, then the lost-pidfile case: start it, delete the pidfile by hand, run make dev again and show it recovering rather than failing to bind.
Paste the real transcript of that recovery — it is the point of fix 2.

EXIT CRITERION
Four generator tests plus three script-behavior cases green, the recovery transcript showing a successful restart after a deleted pidfile, ./... green, vet clean, lint clean.

ROLLBACK
One template, one op, one Makefile branch. Removing them restores current behavior; an installed hook is a file the user can delete, and nothing in the server depends on its presence.

REPORT BACK
The op name and its output records, the hook's exact rejection message, each test's real output, the recovery transcript, which listener-detection tool you found available, and anything you deliberately did NOT do.
