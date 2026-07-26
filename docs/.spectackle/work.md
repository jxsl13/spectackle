---
schema: v1
---

## T-01KYEGD5Y9F2X85Z7Z0KMTDEAH the worktree curve graduates from finding to baseline: 3/3 at 15-19 calls after the pointer and the collision fix
kind: task
state: done
created: 2026-07-26

The bench-curves document carries the worktree row as a finding, not a baseline, pending the guidance and collision fixes. Both landed (T-01KYEBT approved-pointer; B-01KYED3D attach fix through three adversarial review rounds) and the anchored rerun delivers the graduation: n=3 valid=3/3 calls=15/17/19 bytes=1742/1799/2023, zero flow shortcuts (rate was two of four pre-pointer, one of three pre-collision-fix), anchors clean, and every judge edited under its worktree root. Change: docs/bench-curves.md worktree row becomes this baseline with the batch date; the finding paragraph compresses to the resolved history naming both fixes; basic and tricky rows take their anchored regression-batch numbers where fresher. VERIFY: grep the document for the new aggregate line; the finding wording no longer claims an open gap.
