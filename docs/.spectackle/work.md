---
schema: v1
---

## T-01KYEB7H5VEHMR76VTEMXDCSPN the judge reference curves become a versioned document, verified by the first fully anchored cross-scenario regression batch
kind: task
state: active
created: 2026-07-26

The four reference curves — basic, basic with manifest, tricky, worktree — exist only in archive notes, which future sessions and agents will not reread before their next text change; the measurement discipline (n=3, all-valid gate, spreads quoted, nonce anchors) likewise lives in tribal memory. Change: docs/bench-curves.md records per scenario the goal set, the scoring criteria, the current reference numbers with their source batch, the known noise floor, and the operating discipline including the anchor practice prep prints and -nonces consumes; internal/bench package doc points to it. Verification doubles as content: one fresh cross-scenario regression batch — one basic, one tricky, one worktree judge against current main, scored in one anchored command — both exercises the B-01KYEA anchor end to end for the first time live and supplies the documents baseline table from a single build and day rather than stitched history. VERIFY: go test ./internal/bench/ -count=1 green; the batch results and anchor verification in the archive note and the document; grep of the document for each scenario name and for nonce.
