---
schema: v1
---

## B-01KYH3SP63E3NVV5V31G2GTD6K bench prep leaves meter and transcript logs git-trackable; the first sweep commits them and the offline merge then refuses forever
kind: bug
state: draft
created: 2026-07-27
targets: internal/bench/agent.go

OBSERVED (T-01KYH1GK require-3 judge, full forensic trace in its report): the fixtures first active-transition sweep (CommitCode, add-everything-except-.spectackle) git-TRACKS meter.log and transcript.log; every subsequent shim call appends to both unconditionally, so the tree is dirty at every instant; the offline forges merge path runs git checkout main (offline.go:201) which refuses on the dirty tracked files - and NO sanctioned meter.sh call sequence can ever leave the tree clean, because the cleaning call itself appends its own trailer. The judge was structurally blocked from archived with a passing validate verdict on record, stopped honestly at done, and the run scores invalid for a harness reason. EXPECTED: bench artifacts are never git-visible in the fixture - AgentPrep writes a .gitignore (or appends to the fixtures) covering meter.log, transcript.log, meter.sh, brief.md, journal.baseline, trap.hash, scenario, manifest.size BEFORE the judge starts, so the sweep never tracks them. TESTS: after prep, git status in the fixture shows none of the harness files; an active->done->archived drive with interleaved shim calls (each dirtying the logs) completes the offline merge. Also RESCORE ab2-r3 kept as fixture material: with the artifacts untracked its blocker disappears - do not retro-score the run (the judge stopped before archived), just pin the class. VERIFY: build/test/vet/gofmt; the new prep gitignore test; the interleaved-drive e2e. SCOPE: AgentPrep + tests. ROLLBACK: revert.
