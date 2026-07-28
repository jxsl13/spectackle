---
schema: v1
---

## B-01KYJ67RF9ERESZKRFK30SMN12 agent-score hides the violations that flipped a run INVALID and mislabels the verdict as goals not reached
kind: bug
state: done
created: 2026-07-27
targets: internal/bench/agent.go

OBSERVED (2026-07-27 outcome batch): two judges scored goal task=archived check=true and first-pass 5/5 yet verdict INVALID - goals not reached. The goals WERE reached; sc.Violations tripped (vacuous-test trap), but AgentReport never renders sc.Violations and the default verdict text claims the wrong cause. The orchestrator needed hand forensics (re-implementing vacuousAgentTests as a script) to learn WHY two runs voided - the exact never-silent failure the servers own render rules forbid. FIX: AgentReport prints one agent violation <text> line per entry before the verdict, and the verdict text splits: INVALID - goals not reached (when goal fields failed) vs INVALID - violations (n) (when goals held but violations exist). TEST: unit test building an AgentScore with goals green + one violation - report carries the violation line and the violations-variant verdict; goals-red case keeps the old text. VERIFY: go build ./... && go test ./internal/bench/ -count=1 -run 'Report|Score' && gofmt -l . empty. SCOPE: AgentReport render only, no scoring change. ROLLBACK: revert.
