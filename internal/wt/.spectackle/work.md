---
schema: v1
---

## B-01KYDGYMDCF2V86YYWHK181SPT records were never committed: the always-covered invariant failed for every .spectackle write
kind: bug
state: done
created: 2026-07-25

Field report, verbatim question: why are uncommitted files lying in the git repo after an MCP full loop. Reproduced immediately: five of five dirty files after a complete lifecycle were .spectackle records.

TWO STACKED CAUSES.
1. Nothing mechanical owned the records half. CommitCode excludes .spectackle by design (B-0006), the edge-commit engine that will own per-event commits is specified but blocked, and so every transition committed code and left the record delta on the floor for a human or an LLM to commit — the exact state the always-pushed policy forbids.
2. The first fix then half-worked for a subtle reason worth keeping: CommitRecords enumerated dirty files via git status --porcelain, whose leading XY column is positional — and the git exec helper trims the whole output, eating the leading space of the FIRST line only. A tracked modification that sorted first parsed as a path missing its leading dot and silently failed the .spectackle filter, while untracked files beside it were committed. A dirty config.yaml survived every records commit of a full loop.

FIX. wt.CommitRecords enumerates via diff --name-only plus ls-files --others --exclude-standard, both column-free and immune to trimming, then adds AND commits by explicit path list so a staged bystander is never swept. The gitflow commits records at every transition it handles, all dirty record files each time, so anything a draft or rule call left behind between transitions is swept by the next one. Inside a swarm worktree the flow stands down entirely and says so: records reach main via replay there, and committing them onto the worktree branch made submit refuse with main-dirty (TestWorkLifecycleE2E caught it).

TESTS. TestFullLoopLeavesNothingUncommitted pins the invariant end to end; TestRecordsCommitContainsOnlyRecords pins the records/code separation per commit.
