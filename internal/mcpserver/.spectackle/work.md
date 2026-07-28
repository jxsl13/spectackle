---
schema: v1
---

## B-01KYMCHPF6EXZ9FHGZFX5ZNSHK find scope=rule omits the pattern field the documented r-line grammar requires
kind: bug
state: draft
created: 2026-07-28
refs: R-01KYMA7EXME6KAW9B77MJQ4MSD
targets: internal/mcpserver/tools.go

Found by the R-01KYMA7EXME6K gap hunt (FAIL 2), field-count verified in two fixture repos. docs/tools.md line 47 documents r <ruleID> <P> <scopeDir> <text>; rule op=add and get both render the pattern, find scope=rule does NOT: find renders r AUTH-002 src/auth The auth module SHALL... A caller parsing per the documented grammar reads scopeDir as the pattern and the sentence first word as scopeDir. FIX: the find rule renderer emits the pattern like the other two paths (one shared ruleLine helper if they have diverged). TEST: pin that find scope=rule and get render the same field count and the same pattern token for the same rule. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1.
