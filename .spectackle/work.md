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

## B-0004 MergeMain hardcodes the branch name main, so submit silently merges a stale ref and dies at the fast-forward on repos developing on another branch
kind: bug
state: active
created: 2026-07-25
targets: internal/wt/wt.go

DEFECT
wt.MergeMain runs git merge --no-edit main inside the worktree. When the primary checkout's development branch is not literally named main (observed live: claude/repo-mcp-spec-driven-3l93dx at fb11265, with a stale local main 77 commits behind on a diverged lineage), the merge is a silent no-op against the stale ref, GATE 2 passes trivially, and FFMain then fails with exit 128 diverging-branches because the worktree branch never picked up the real tip. Every worktree submit in such a repo fails identically; T-0115 reproduced it end to end. Secondary observation: workSubmit leaves the coord worktree state stamped gating/integrating on early return, which is cosmetic (submit does not check it) but misleading in swarm output.

CAUSE
The integration target is a naming convention, not a resolved fact. The correct target is whatever branch the primary checkout has checked out — already discoverable from the worktree via CommonRoot + symbolic-ref.

FIX (decision)
MergeMain resolves the primary checkout via CommonRoot(wtRoot) and merges its current branch (symbolic-ref --short HEAD), falling back to the literal main only when resolution fails (non-worktree callers, tests) and to the HEAD sha when the primary checkout is detached. Signature unchanged, so the leased swarm.go call site is untouched. Rejected: passing the branch through workSubmit — correct too, but needlessly edits a file another agent holds a lease on, and every future caller would have to re-derive the same fact.

VERIFY
go test ./internal/wt/... -race with a new regression test (primary checkout on a non-main branch, commit lands on it after worktree creation, MergeMain in the worktree picks it up so FFMain succeeds); go test ./...; live: T-0115 and T-0111 submits complete.

ROLLBACK
One function body; reverting restores prior behavior. No schema or record change.

## B-0005 CommitCode misreads unstaged .spectackle changes as staged: the shared git() helper trims the leading space off the first porcelain status line
kind: bug
state: active
created: 2026-07-25
targets: internal/wt/wt.go

DEFECT
wt.git() returns strings.TrimSpace over the whole combined output. For git status --porcelain that strips the leading space of the FIRST line only, so an unstaged entry like ' M .spectackle/anchors.tsv' arrives as 'M .spectackle/anchors.tsv' and CommitCode's l[0] != ' ' staged-detection reads it as staged. When the only remaining diff after the code-only add is .spectackle state (near-universal at submit time: the code commit already exists from a prior gate round), CommitCode issues a commit with nothing staged and git exits 1 — work op=submit fails with ! WT E commit. Reproduced live by T-0111 (byte-level transform confirmed); would also have hit T-0115's retry.

CAUSE
Positional-whitespace parsing of an output channel that a shared helper normalizes for unrelated callers (rev-parse et al. want the trailing newline gone).

FIX (decision)
Stop parsing porcelain for stagedness: git diff --cached --quiet exits non-zero exactly when the index differs from HEAD, immune to any output trimming. Rejected: un-trimming git() (every other caller depends on the normalization); a second raw-output helper (an exit-code probe is strictly simpler than a second output contract).

VERIFY
go test ./internal/wt/... -race with a new regression test (worktree whose only dirt is an unstaged .spectackle file: CommitCode must report committed=false with no error — fails before the fix, passes after); go test ./...; live: T-0111 and T-0115 submits complete.

ROLLBACK
One detection block in CommitCode; reverting restores prior behavior. No schema or record change.

## B-0006 worktree .spectackle live state blocks MergeMain: seeded uncommitted by copyBundles, excluded by CommitCode, refused by git when main's tip touches the same files
kind: bug
state: active
created: 2026-07-25
targets: internal/wt/wt.go

DEFECT
copyBundles snapshots main's live .spectackle bundles into a fresh worktree as uncommitted working-tree files (deliberate: bundles may be ahead of HEAD), and CommitCode's codeOnly pathspec keeps them out of every branch commit (deliberate: replay.Run reconciles record state semantically, git never merges it). Net effect: every worktree carries permanent uncommitted .spectackle diffs. While MergeMain silently merged a stale ref (B-0004) this was invisible; merging the real advancing tip — whose commits touch the same journal files — makes git abort with would-be-overwritten-by-merge. Deterministic for every worktree by construction; reproduced by T-0115 and expected identically for T-0111.

CAUSE
The live record snapshot and the git merge share paths but not ownership: the files are replay's input and must survive verbatim, yet they sit in git's working tree where a merge is entitled to update them.

FIX (decision)
MergeMain preserves and restores: before merging (only when no merge is already in progress), save the bytes of every modified-tracked and untracked working-tree path under a .spectackle dir, clear them (checkout for tracked, remove for untracked), merge, then write the exact bytes back — replay's input survives verbatim, the merge sees a clean tree, and replay stays the sole owner of record-state reconciliation. Rejected: discarding the local .spectackle diffs (loses the worktree's own record delta — item moves, rule events — that replay must apply); git stash (pop conflicts on the same paths reintroduce the problem nondeterministically); committing bundles in worktrees (contradicts the standing design that git never carries record-state merges).

VERIFY
go test ./internal/wt/... -race with a regression test (worktree with local journal edit, main commits a change to the same file, MergeMain succeeds, local bytes preserved, FFMain follows); go test ./...; live: T-0111 and T-0115 submits complete.

ROLLBACK
One helper and one call inside MergeMain; reverting restores prior behavior. No schema or record change.
