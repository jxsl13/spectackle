---
schema: v1
---

## B-01KYFSZ7A6FZV9QY6SK88AMJ9V rule composer accepts a trigger already starting with WHEN and emits WHEN WHEN
kind: bug
state: done
created: 2026-07-26
targets: internal/spec/author.go

OBSERVED: seven root rules (ORCH-PROOF-001, ASK-SURFACE-001, AGENT-ISOLATION-001, GROUND-LADDER-001, GATE-AUTHORITY-001, ORCH-SYNC-001, COMPLETE-DIMS-001) carried the sentence prefix WHEN WHEN because rule op=add composed pattern E by prepending WHEN to trigger text that itself began with WHEN. All seven were repaired 2026-07-26 by rule op=edit re-composition; spec lint W-level checks did not catch the doubled keyword. EXPECTED: the composer normalizes the trigger (strip a leading WHEN/WHILE/IF case-insensitively before composing) or refuses with an ARG error naming the doubled keyword; spec lint gains a doubled-keyword check so an already-damaged file surfaces as a finding. REPRO: rule op=add pattern=E trigger="WHEN x happens" system="y" response="z with artifact F()" then read the composed sentence. Note the open draft T-01KYD2XQG6E38 (rule op=edit recomposition) also targets internal/spec/author.go - coordinate or fold this into it.
