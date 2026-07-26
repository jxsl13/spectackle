---
schema: v1
---

## B-01KYE0RCKEFGBVT4GQ06GAR3FD judge fixture leaves root files uncovered, so the brief goal check-reports-ok is not literally reachable
kind: bug
state: draft
created: 2026-07-26

Found by live judge C: the v2 seeds cover api, api/handlers, store and cli but not the root main.go, so every check on a fresh judge workspace answers with the uncovered gap line instead of plain ok. The brief states the third goal as a final check call reports ok — a diligent agent takes that literally and rabbit-holes into rule-writing to clear the gap (judge C spent 29 extra calls there; judges A, B and D shrugged and stopped, which happens to match the scorer, whose CheckOK accepts any E-free output). Brief and scorer must agree and the straight path must exist: add a root-context rule to seedRules (the scripted bench does exactly this with its rule/add step for the same reason), bumping the seeded totals to 8 rules across 5 dirs; update TestSeededFixtureRendersMultiDirCleanInventory accordingly. VERIFY: a fresh agent-prep workspace answers check with plain ok; the multi-dir inventory test pins total=8 dirs=5 findings=0.
