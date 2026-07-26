---
schema: v1
---

## B-01KYFPNCK2E2QVSAF3QDS1W11S http CLI path silently drops SPECTACKLE_AGENT for identity-bound verdicts
kind: bug
state: draft
created: 2026-07-26
targets: cmd/spectackle/main.go, internal/mcpserver/validate.go

REPRO: SPECTACKLE_AGENT=cross-val-87zn spectackle call -http <addr> validate op=verdict records the verdict as the SERVER agent (dogfeed-orchestrator), while the same env on a -root direct call stamps correctly. OBSERVED during T-01KYD87ZN cross-verification: the independence evidence exists only in the findings text, not in the recorded identity. EXPECTED: identity-bound events (validate/grill verdicts) driven over -http must carry the caller identity - either forward SPECTACKLE_AGENT per call (header or input field) or refuse verdict ops over -http when the caller identity equals the item author, forcing the caller to pick a channel that records truthfully. Silent identity substitution is the defect: it fabricates independence. DESIGN NOTE: per-call agent field must not allow spoofing an arbitrary identity onto OTHER event kinds; scope it to verdict-bearing ops or sign it with the ephemeral-agent machinery (ag-[0-9a-f]{4}).

## ADR-01KYFYGVSRFX4B9B2YJ44QSBS8 live probe: should the widget cache be bounded
kind: adr
state: done
created: 2026-07-26
decision: yes bounded
status: accepted

kind: radio
option: yes bounded
option: no unbounded
choice: yes bounded
