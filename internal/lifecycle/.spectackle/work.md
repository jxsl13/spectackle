---
schema: v1
---

## T-01KYDAS36XFHJ8X3H1Q8Y845FF exhaustive, deterministic transition matrix over every state pair
kind: task
state: done
created: 2026-07-25
targets: internal/lifecycle/lifecycle.go, internal/lifecycle/lifecycle_test.go

Requirement: every state transition to every other state is tested deterministically.

WHAT IS MISSING TODAY, and it is not coverage of the happy paths. internal/lifecycle has scenario tests — forward skip, reopen, reject and revoke, archived terminal, the three ResolveBlocked outcomes — and they are good tests. What none of them establish is the CROSS PRODUCT: for each of the eight states as a source, what every one of the eight destinations does. The gaps that shape leaves are exactly where a state machine rots, because a scenario test only pins the paths somebody thought to walk.

THE STATES, all eight: the six ordered main states draft < submitted < approved < active < done < archived, plus the two side states rejected and blocked. Eight sources times eight destinations is 64 cells, self-transitions included; every one gets an expected outcome.

THE RULES THE MATRIX MUST ENCODE, read off Allowed and Move rather than invented:
  any forward hop in the total order is legal in one call, skips included
  done->active is the single backward hop kept outside rejection, and it counts against the feedback round budget
  rejected is reachable from every main state except archived, and REQUIRES a note
  rejected is revocable into draft, submitted, approved or active, never into done or archived
  archived is terminal: nothing leaves it
  blocked refuses every Move in AND out; only ResolveBlocked exits it, and blocked is never a legal Move destination from anywhere
  self-transitions are refused for every state

TWO PROPERTIES, not one. The matrix asserts what Move DOES. It must also assert that Allowed AGREES with Move for all 64 cells — Allowed is advertised to callers, it composes the refusal messages and the tool surface reads it, so a cell where Allowed promises a transition Move rejects (or the reverse) is a real defect and is invisible to any test that exercises only one of the two.

DETERMINISM, the word in the requirement, and the part with actual traps:
  IDs are UUIDv7 since ADR-0013 — capture what Draft returns, never write a literal.
  done->active consumes a feedback round, so set Feedback.MaxRounds high enough that the matrix never trips ErrRoundsExhausted by accident; that exhaustion is its own named case, not a surprise inside a table row.
  archived and rejected items leave work.md, so each row must build its source state from a FRESH workspace — no shared fixture that earlier rows have already mutated, and no dependence on row order.
  blocked can only be reached through Escalate, so the fixture for that source is deliberate setup, not a Move.
  iterate over an ordered slice of states, never a map, or the failure output reorders between runs.

TESTS
  the 64-cell matrix: each cell asserts allowed-and-landed or refused-and-unmoved, and a refusal must leave the stored item in its original state (a refused Move that still wrote is the worst failure mode here).
  Allowed(from) returns exactly the expected set for each of the eight sources, as a literal, so the advertised contract is pinned rather than derived from the same table.
  Allowed and Move agree across all 64 cells.
  rejected as a destination without a note is refused from every state that otherwise permits it.

VERIFY: go build ./... ; go test ./internal/lifecycle/... -race -count=1 ; go test ./... -race ; go vet ; gofmt -l (empty) ; spectackle lint . (POSITIONAL path).
SCOPE: internal/lifecycle tests. Production code changes ONLY if the matrix uncovers a genuine disagreement — in which case report it rather than quietly adjusting the test to match the code.
ROLLBACK: test-only; delete the file.
