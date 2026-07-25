---
schema: v1
---

## B-01KYDQTEM9EN8VZN2VA9G9J7H5 TestDialHTTPMatchesStdio flakes under full-suite parallel load, three sightings
kind: bug
state: done
created: 2026-07-25

Three occurrences across two days, always in the full go test -race ./... run, never in isolation (three consecutive isolated runs pass each time it is checked). The test dials an HTTP server; under parallel suite load the dial or the server startup races a timeout. Suspects: a fixed dial timeout too tight for a loaded machine, or a port-availability race with concurrently running server tests, possibly including a resident dev server on 7412 or 7413 outside the suite. Not yet diagnosed — this record exists because the third sighting makes it a pattern, and a flaky test in the local gate erodes trust in the gate exactly like the fuzz flake did in CI (B-01KYDP precedent: exploratory or flaky steps do not belong in deterministic gates). VERIFY for the eventual fix: the full race suite passes twenty times consecutively on a loaded machine.
