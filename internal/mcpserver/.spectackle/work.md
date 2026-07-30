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

## B-01KYRVXQ02FDH9YBAFG64SH13N knowledge export mixes the artifact and its ok line on one stream, so export piped into apply fails with an unmappable line number
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/knowledge.go

Found by an independent verifier while checking something else, and it cost that verifier a wrong hypothesis before it identified the cause - which is the real damage here.

OBSERVED. knowledge op=export writes the artifact to stdout AND appends its own ok export record line to the same stream. Piping that stdout into op=apply body= therefore feeds the record line to the YAML parser, which fails with: yaml: line 17: could not find expected colon. The line number is ENTRY-RELATIVE, so it points at a coordinate the caller cannot locate in what it sent - the caller counts lines in its own input and finds nothing wrong at line 17.

EXPECTED. Either the artifact is the only thing on stdout so the obvious pipe works, or the refusal explains that the input carries a trailing record line and names it. The documented path= route works in both directions, so this is not a broken feature - it is a shape that invites a wrong call and then misdirects the caller who makes it.

WHY IT MATTERS FOR TOKEN ECONOMY. An agent that pipes export into apply gets a parser error naming a line it cannot find, and the cheapest recovery is to re-read both artifacts and the tool docs. The error is worse than no error, because it sends the reader to the wrong place. This is the same failure class as a refusal that renders a success-shaped line: the output describes a world the caller is not in.

FIX DIRECTION. Decide which stream owns the record line. Cleanest is that an op whose output IS a document emits only the document on stdout and its ok line on stderr, matching how a caller would naturally compose it. If the two must share a stream, the apply parser should detect a trailing record line and say so by name instead of surfacing a raw YAML error, and any line number it reports must be in the coordinate system of the input the caller supplied.

VERIFY. A test that pipes export output directly into apply and asserts it either succeeds or refuses with a message naming the trailing record line; plus an assertion that any reported line number resolves against the caller input.

## B-01KYSDBZTEF1AS4KG1ZR0P14G7 the records-path exemption in the scope gate is anchored at the repo root, so a nested context dir own records block the archive of the item that caused them
kind: bug
state: done
created: 2026-07-30
rounds: 2
targets: internal/workspace/workspace.go, internal/workspace/workspace_test.go, internal/mcpserver/gitflow.go, internal/mcpserver/gitflow_test.go, internal/mcpserver/swarm.go

CORRECTED after reading the code: the exemption is not missing, it is root-anchored. That is a sharper and smaller defect than first filed.

The transition gate refuses a move when the working tree has changed files outside the item declared targets, and it already tries to exempt server-owned records:

  if f == workspace.Dot || strings.HasPrefix(f, workspace.Dot+"/") { continue }

That matches .spectackle and .spectackle/... - the ROOT context only. Context dirs are nested, so a non-root context records live at internal/mcpserver/.spectackle/work.md, which matches neither branch and is therefore counted as undeclared work. The intent was right; the path test only covered one case.

DEADLOCK REPRODUCED LIVE, twice in one session. B-01KYS6Y5NKF42 declared three code targets all under internal/mcpserver, so the server re-scoped the record into that context dir and wrote the record own block into that dir work.md and journal. The next archive refused: 2 changed files outside the declared targets, naming internal/mcpserver/.spectackle/journal.ndjson and work.md. The item was blocked by the server writing the item own record. No transition clears it, because the same gate guards every direction, and the caller cannot declare those paths as targets without lying about what the work touches. The only exits are a manual git commit or discarding server state. Note the perverse incentive: the more precisely an item scopes itself to one subtree, the more likely the server re-scopes it into that subtree and deadlocks its own archive.

THREE SPELLINGS, ONE PREDICATE - the actual root cause. gitflow.go:1086 and swarm.go:607 are root-anchored; gitflow.go:740 uses strings.Contains(f, Dot+"/") and does handle nesting, though its companion HasPrefix(f, Dot) would also match a file literally named .spectacklefoo. Three hand-written spellings of is this path inside a records directory, differing in exactly the case that bites. This is the same drift-by-duplication shape as the truncation marker and the commit-subject composer, both fixed this week by introducing one composer.

FIX. Add one predicate to internal/workspace - IsRecordsPath - that tests whether any PATH SEGMENT equals Dot, so root and nested behave identically and a .spectacklefoo file is not matched. Use it at all three sites. Do not fix gitflow.go:1086 alone: swarm.go:607 has the identical latent bug in dirtyOrphanGuard, where a nested context records write would be reported as uncommitted work from another holder and push the caller toward force=true, which discards.

SECOND, SEPARATE QUESTION in the same gate: it counts a sibling _test.go as outside a targets list that names its source file. Every record here must ship tests, so this forces every targets list to enumerate the test file for a file it already declared - three reject-to-draft-widen cycles were spent on it in one session. Decide it deliberately rather than inheriting it: either a declared source path implicitly covers its _test.go sibling, on the grounds that mandatory tests are not undeclared work, or scope stays strictly explicit and the refusal at least prints the widened targets list so the caller does not reconstruct it by hand. The current behavior is pinned by no test, which is why it reads as an accident.

VERIFY. An item whose targets all sit inside one context dir, so the server re-scopes it: archive must succeed with the server pending records writes present - that is the deadlock, and it must be a test rather than a manual check. A table test on IsRecordsPath covering .spectackle, .spectackle/x, a/b/.spectackle/x, and the near-miss .spectacklefoo. A dirtyOrphanGuard test with a nested context records write asserting it is not reported as foreign uncommitted work. Plus whatever the sibling decision needs to pin it.

## B-01KYSK7HQFFPM8538HAWGRS0P6 reconcileClosureBranch has no test coverage at all, and the records exemption now excuses real files at any depth rather than only at the root
kind: bug
state: draft
created: 2026-07-30
targets: internal/mcpserver/gitflow.go

Two residuals surfaced by mutation verification of B-01KYSDBZTEF1A. Neither is a bypass and neither blocked that record; both are worth closing deliberately rather than by accident.

RESIDUAL 1, an untested reconcile path. reconcileClosureBranch has ZERO test coverage - a search for reconcile in gitflow_test.go returns no hits. B-01KYSDBZTEF1A changed a line inside it, replacing a root-anchored git add -A -- dot-spectackle pathspec with staging the exact files the conflict classifier resolved, because once the classifier became nesting-aware the old pathspec failed with pathspec did not match any files on a nested-only records conflict, leaving the file unmerged and aborting the merge. That fix was verified only at the git-mechanics level by a verifier own harness, plus by reading. It is pinned by nothing.

WHY IT IS HARD, and therefore why it is filed rather than done: the path runs during an online archive closure and wants a live forge, so a test needs either a fake forge or a seam that lets the reconcile run against a local repo with a real merge conflict. The second is probably the right shape and is a small refactor: the function already takes its git operations through a closure, so injecting a repo root and skipping the forge call would make the conflict-classify, checkout-theirs, stage, commit sequence testable in a temp repo. Measured behavior to pin once testable: a nested-only records conflict resolves and commits; an empty inside set makes git add with no pathspecs exit 0 and stage nothing, after which the commit correctly fails closed; a modify-delete or rename-rename records conflict fails at the preceding checkout-theirs in both the old and new forms.

ONE MORE MEASURED DIFFERENCE worth recording so nobody re-derives it: the old pathspec also swept an untracked or locally modified ROOT records file into the reconcile commit, which the new form leaves untracked. That is not a regression, because gitCommitRecords runs immediately afterward in gitFlowMerge and is nesting-aware, so the file lands one step later - but a test should assert that rather than trusting the sequencing.

RESIDUAL 2, the exempt surface widened from the root to every depth. workspace.IsRecordsPath exempts any path with a dot-spectackle SEGMENT, so a real file placed under any such directory at any depth - notes.txt, or a .go file - passes the scope gate and is then committed under the server records subject. Measured: three such paths pass at exit 0.

WHY IT WAS JUDGED ACCEPTABLE, and the caveat. The ROOT case behaved identically before the change, since the old expression exempted dot-spectackle slash anything too, so this widens an existing hole rather than opening one; and the Go toolchain ignores dot-directories entirely, confirmed by a verifier placing syntactically invalid Go under one without breaking go build. The caveat is real though: in a non-Go language a script or asset under a records directory IS reachable, and the surface is now every directory depth instead of one. So the exemption rests on a language-specific accident that the predicate does not state.

FIX DIRECTION. Exempt records by NAME rather than by directory: the server writes work.md, spec.md, journal.ndjson and its known siblings, so the gate could exempt those filenames inside a dot-spectackle segment and refuse anything else there. That keeps the deadlock fix - those are exactly the files that deadlocked - while removing the smuggling surface at every depth including the root, which is a strict improvement over the pre-existing behavior. VERIFY: a .go file and a shell script under a records dir at root and nested depth must both be REFUSED, while work.md and journal.ndjson at both depths are exempt.
