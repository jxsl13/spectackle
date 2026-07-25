---
schema: v1
---

## T-01KYDPSW87FKX8FWQBVNDN89TT the harness: generated all-state fixtures, a scripted lifecycle driver over the real tool surface, token and validity metering, A/B against two builds
kind: task
state: done
created: 2026-07-25
parent: P-01KYDPRXQSF8XAP1HCHY7390T8

IMPLEMENTER: the orchestrator, in the open worktree — the harness is measurement infrastructure whose design decisions ARE the task.

WHAT TO BUILD, package internal/bench plus a bench subcommand or make target.
1. Fixture generator: synthetic workspaces exercising all eight states and the tricky shapes named in the parent proposal. Generated fresh into a temp dir per run — no checked-in fixture rot. Each fixture declares which states and shapes it covers; the harness refuses to report full coverage unless the union covers all eight.
2. Driver: a scripted sequence of REAL tool calls (offline git mode, no network) walking each fixture through a complete lifecycle including one rejection with revocation and one blocked escalation. Captures every result byte per call.
3. Metering: bytes and estimated tokens (bytes over four is acceptable as the estimator, named as such) total and per tool; validity computed from the workspace after the run — expected end states via the tool surface, check ends ok, no dangling refs; completeness as presence of every promised record family (i, g, and the never-silent gate reasons).
4. Report: one dense text block per run — per-tool tokens, totals, validity booleans — and an A/B mode that runs the same script against two server BINARIES and prints the deltas. Exit non-zero when validity fails, so the harness itself is scriptable.

VERIFY: go build ./... ; go test ./internal/bench/... ; go test ./... -race ; the harness run over the generated fixtures reports full state coverage and validity ok on the current build; gofmt -l empty ; spectackle lint . positional.
SCOPE: internal/bench, cmd wiring for the subcommand or make target, docs. No changes to emitted texts yet — the baseline must be measured by the harness that later judges the changes, not alongside them.
ROLLBACK: the harness is additive tooling; deleting the package removes it whole.
