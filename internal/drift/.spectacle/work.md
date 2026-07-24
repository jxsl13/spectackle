---
schema: v0
---

## P-0024 rule edit must reconcile the anchor set — stale applies rows survive forever
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: go:mcpserver.Server.stampAnchors, go:drift.Upsert, go:drift.Save

Found live via dogfooding: `rule op=add` with a mistyped node ID (go:index.Indexer.IndexAll — capital I, never indexed) wrote a pending anchor row; the follow-up `rule op=edit` with the corrected applies list (go:index.indexer.IndexAll) ADDED the good row but left the stale one — anchors.tsv now carries both and `check` reports '1 anchors pending' forever. Root cause: mcpserver.Server.stampAnchors only drift.Upsert-s the rows for the NEW applies list; nothing removes rows of the same rule that are absent from it. Fix: on `rule op=edit` (and retire), reconcile — drop every anchors.tsv row whose rule ID matches but whose node is not in the new applies set, then upsert the new rows. drift package grows a Reconcile(anchors, rule, keep []NodeID) helper (pure, testable); replay.stampAnchors must apply the same semantics so worktree replays converge to identical anchors.tsv.

## T-0044 drift.Reconcile: rule edit/retire drops stale anchor rows
kind: task
state: approved
created: 2026-07-24
parent: P-0024
targets: go:mcpserver.Server.stampAnchors, go:drift.Upsert

SCOPE (only these files): internal/drift/drift.go, internal/drift/drift_test.go, internal/mcpserver/tools.go (stampAnchors + ruleEdit + ruleRetire ONLY), internal/replay/replay.go (stampAnchors only), internal/replay/replay_test.go, internal/mcpserver/tools_test.go (one new test).

BUG (live in this repo): rule op=add wrote anchor row for a mistyped never-indexed node (go:index.Indexer.IndexAll); rule op=edit with corrected applies added the good row (go:index.indexer.IndexAll) but left the stale one. anchors.tsv keeps both; check reports '1 anchors pending' forever. Current repo state STILL CONTAINS this stale row in .spectacle/anchors.tsv (rule IDX-001, node go:index.Indexer.IndexAll, span 0-0) — your test fixture can mirror it, and the LIVE row must disappear once the fixed binary re-runs rule op=edit id=IDX-001 (the orchestrator will do that live re-stamp after you finish; do NOT hand-edit anchors.tsv).

FIX (DRF-001):
1. internal/drift/drift.go: add `func Reconcile(anchors []Anchor, rule string, keep []graph.NodeID) []Anchor` — pure: returns anchors with every row of `rule` whose Node is NOT in keep removed; rows of other rules untouched; preserves order. Table-driven test in drift_test.go (cases: drop one of two, keep all, rule absent, empty keep drops all rows of the rule).
2. internal/mcpserver/tools.go stampAnchors(ruleID, sentence, applies): before the per-applies Stamp/Upsert loop, load anchors, apply Reconcile(anchors, ruleID, applies-as-NodeIDs), save, then stamp as today. This makes add idempotent too (add with same rule reconciles to exactly the applies set). ruleRetire must call Reconcile with keep=nil so a retired rule loses ALL its anchor rows (check today would keep flagging them; verify current retire behavior and cover it in the new tools_test.go test).
3. internal/replay/replay.go stampAnchors: same reconcile-before-stamp semantics so a worktree replay converges to the identical anchors.tsv (replay_test.go: replay a journal that edits a rule's applies; assert the stale row is gone).

CONSTRAINTS: do not touch other tools, langspec, graph, index, .spectacle/ (server-owned). Keep comment style (contracts, not history). Never commit/push.

EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/drift/ ./internal/mcpserver/ ./internal/replay/ green; make lint-specs clean.
