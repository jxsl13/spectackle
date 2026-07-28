---
schema: v1
---

## B-01KYJ66VSQF8XVZ4PCP8XNARW9 work op=start promises submit from any process but the lease binds to an undocumented ephemeral identity
kind: bug
state: done
created: 2026-07-27
targets: internal/mcpserver/swarm.go

FOUND by ALL THREE outcome judges independently (2026-07-27 batch): work op=start renders ok edit/build/bench ONLY under this root; check until ok, then work op=submit item=X (any process) - but every stdio CLI call mints a fresh ephemeral agent id, so the very next submit refuses with WT E worktree held by ag-XXXX - run with SPECTACKLE_AGENT=ag-XXXX to reattach. The env var name and the holder id are surfaced ONLY in that refusal; every judge lost a retry loop reverse-engineering it. The (any process) claim is true only WITH the env var - the hint omits the one fact that makes it true. FIX: the work op=start success render names the binding immediately: replace (any process) with (any process carrying SPECTACKLE_AGENT=<agent>) using the actual holder id; the submit hint line carries the same. One line changed, no mechanics. Per RENDER-PARITY-001 this stays one line. TEST: pin the start render contains SPECTACKLE_AGENT= and the holder id (extend the wipeguard or swarm start test asserting the new text); the judges retry loop becomes structurally impossible to hit for a reader of the hint. VERIFY: go build ./... && go test ./internal/mcpserver/ -count=1 -run 'Work|Wipe' && gofmt -l . empty. SCOPE: the start/submit hint strings + test pins. ROLLBACK: revert.
