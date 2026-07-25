---
schema: v0
---

## B-0010 lint exits 0 when its path cannot be read, so a mistyped invocation passes CI while linting nothing
kind: bug
state: draft
created: 2026-07-25
targets: cmd/spectackle/main.go

DEFECT
The lint subcommand takes a positional PATH. Given a flag-shaped argument instead, e.g. lint -root DIR, it treats the flag as the path, prints lint: lstat -root: no such file or directory, and exits 0. Reproduced on the shipped binary: stdout carries the error, echo $? reports 0. A CI step, Makefile target or agent script written with the wrong form therefore reports success while never linting a single spec file. The documented contract is the opposite: lint exits 1 on errors, which is why CI relies on it as a gate.

IMPACT
Silent, not loud. The failure mode is a green pipeline with no linting, which is strictly worse than a red one. This repository's own CI and Makefile happen to use the correct positional form, so nothing is currently unguarded here, but an agent copying the wrong form into a task template would disable the gate invisibly. Found by the T-0131 implementer, whose task template carried the wrong form.

CAUSE
An unreadable path is treated as an empty walk result rather than as a hard error, so the zero-findings success path is reached.

FIX (decision at implementation)
A path that cannot be stat'd is a usage error: report it and exit non-zero. Consider also rejecting arguments that begin with a dash outright, since lint has no flags and a dash-prefixed path is far more likely a mistake than a real file name.

VERIFY
Regression test over the CLI: lint with a nonexistent path exits non-zero; lint with a valid path and a clean bundle still exits 0; the existing exit-1-on-findings behavior is unchanged.

ROLLBACK
One error path in the subcommand; reverting restores the current silent success.
