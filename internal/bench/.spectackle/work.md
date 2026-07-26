---
schema: v1
---

## T-01KYDZ724PE9P98MDXT5Y8BT0M agent-judge stage: bench prepares a metered fixture and brief for an independent agent and scores its run mechanically
kind: task
state: done
created: 2026-07-26

Stage 4 of P-01KYDP, the only honest judge of GUIDANCE quality: a scripted driver cannot get lost, an agent can. Two new bench modes wired through cmd/spectackle. bench -agent-prep DIR: generates the v2 fixture with seeds into DIR, writes brief.md (the FIXED task brief: allowed command shape, tool NAMES only with zero semantic coaching — semantics must come from the server texts under judgment; goals: one task titled agent judge run taken to archived, one bug titled bogus report rejected with a note, finish when check is ok) and meter.sh (a shim that passes through to the real binary while appending bytes, exit code, and argv per call to meter.log), printing both paths. bench -agent-score DIR: reads meter.log (call count, total bytes, token estimate) and judges the workspace mechanically — loads the records and asserts the named task is archived and the named bug is rejected, runs check through the real binary and applies the E/W taxonomy — then prints dense agent lines and a verdict; exit non-zero when the goals are not met. The judge score is comparable across server builds because brief and fixture are identical by construction. Orchestrator-side agent spawning stays OUTSIDE the repository: the binary only prepares and scores. Unit tests: prep produces brief.md, meter.sh executable, and a seeded fixture; a scripted simulation (driving the shim via the real tool surface to the goal states) scores valid with a positive byte count; a fixture left short of the goals scores invalid. VERIFY: go test ./internal/bench/ -count=1 green including the simulation; go vet; a live judge run with at least two independent agents against the current build recorded with call counts, bytes, and verdicts in the archive note.
