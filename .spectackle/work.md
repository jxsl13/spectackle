---
schema: v0
---

## D-0003 Rebranding spectacle→spectackle: wie tief? Der Go-Modulpfad (github.com/jxsl13/spectacle) muss zum GitHub-Repo-Namen passen — ein kompletter Modulpfad-Rebrand setzt voraus, dass du das Repo auf GitHub in spectackle umbenennst. Der .spectacle-Workspace-Ordner ist zudem das persistierte Format bestehender Nutzer-Repos.
kind: decision
state: done
created: 2026-07-24

kind: radio
option: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um
option: brand — Binary spectackle + MCP-Servername + Docs/README/goreleaser; Modulpfad und .spectacle-Ordner bleiben (nicht-brechend)
option: brand+dir — wie brand plus .spectackle-Ordner mit Legacy-Fallback; nur Modulpfad bleibt
choice: full — Binary+Servername+Docs+Modulpfad+.spectackle-Ordner (mit .spectacle-Legacy-Fallback); du benennst das GitHub-Repo um

## D-0004 wazero/WASM tree-sitter backend: D-0002 deferred it based on R-0004 (binary size + latency vs the langspec regex chain, which since then reached 30 languages incl. ObjC/Metal/GLSL and cleared the M4 perf gate 7-15x). 'nix deferred' — reopen and build it now, or keep deferred as an explicit, measurement-justified choice?
kind: decision
state: done
created: 2026-07-24

kind: radio
option: keep-deferred: langspec covers 30 languages approximately; wazero buys full C/C++ ASTs at a real binary-size/latency cost R-0004 measured as not worth it — leave as documented, not silent
option: reopen-poc: mint a fresh wazero C-PoC task (parity oracle vs the C langspec chain, binary+latency budget) to re-measure now that langspec is far larger — decide on data
option: commit-full: build the wazero/WASM parser backend now as the M6 target regardless of the earlier measurement
choice: reopen-poc: mint a fresh wazero C-PoC task (parity oracle vs the C langspec chain, binary+latency budget) to re-measure now that langspec is far larger — decide on data

## P-0056 ADR as a first-class kind: rename decision->adr / D->ADR, full ADR fields, searchable, migrate
kind: proposal
state: draft
created: 2026-07-24
grilled: 2026-07-24

User requirement (structured, decided): architecture decisions must be first-class structured records in spectackle, named ADR (industry term), defined+searched via the MCP, never unstructured docs markdown. Scope decided by user: FULL migration of existing records AND FULL ADR fields. Four coupled parts: (1) RENAME kind decision->adr and ID letter D->ADR everywhere: internal/item/item.go (kinds map 'decision':'D' -> 'adr':'ADR'; IDRe '^[PTBRD]-\d{4}$' -> '^(?:ADR|[PTBR])-\d{4}$' to allow the multi-char prefix; any letter-parsing helpers), internal/lifecycle/lifecycle.go (Draft(...,'decision',...) -> 'adr'; comments), the draft-tool kind enum + decide tool (mints kind adr, ID ADR-NNNN), state/find rendering ('i <id> adr ...'), and the server instructions/prompts wording. (2) FULL ADR FIELDS: extend the item model + work.md block serialization with structured Context, Decision, Consequences, Status (classic ADR template) as first-class fields; the decide tool's ask/answer captures Context+options and records Decision+Consequences+Status on answer; get/state/find render them. (3) SEARCHABLE: find scope=adr in scopeKinds + findIn schema, AND the sync indexer must feed kind=adr into FTS (internal/sync feedWork — this was the real gap T-0082 found for decision; carry it forward for adr), so ADR bodies incl. context/consequences are queryable. (4) MIGRATE: rewrite existing D-0002/D-0003/D-0004 in root .spectackle/work.md + journal.ndjson to ADR-0002/03/04 (one-time pre-v1 migration of this repo's own dogfood data), reindex, verify check ok + find scope=adr returns them; and migrate the wazero design rationale from docs/design-wasm-parsers.md into ADR-0004's structured Context/Decision/Consequences (the doc's Recommendation section becomes the ADR, leaving the measurement appendix). Exit criterion (explicit): draft kind=adr mints ADR-NNNN with Context/Decision/Consequences/Status; decide ask/answer populates them; find scope=adr returns ADRs by body text incl. consequences; the three legacy items are ADR-000x and check is ok; go test ./... -race green; ./bin/spectackle lint . clean; docs/tools.md decide+find sections document adr/ADR and the fields. Rollback: revert diff; migration is idempotent string rewrite. Scope: internal/item/**, internal/lifecycle/lifecycle.go, internal/mcpserver/** (decide/find/draft/state/tools + tests), internal/sync/sync.go, docs/tools.md, docs/design-wasm-parsers.md, root .spectackle/ (migration). Large + coupled: staged as tasks; orchestrator owns moves + verifies the migration.

## P-0057 server walk must skip .claude — agent worktrees pollute spec discovery + FTS cache
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

The worktree-isolation model (user-chosen) has a blocking gotcha found live: git worktrees created by the Agent tool live at .claude/worktrees/<id>/ INSIDE the repo, each a full checkout with its own .spectackle bundles. spectackle's directory walks descend into them: spec discovery doubled to 29 files / 111 rules with 50 duplicate-ID findings while a worktree existed, and the FTS cache retains .claude/worktrees/... journal/rejection entries even after the worktree is removed and reindex runs (they keep surfacing in research/grill/find as duplicates). Four walk skip-lists omit .claude: internal/workspace/workspace.go (ContextDirs, ~line 203 case), internal/spec/cascade.go (~line 85 case), internal/index/indexer.go (ignoreDirs map, ~line 86), internal/mcpserver/tools.go (check coverageGaps switch, ~line 1056). Fix: add '.claude' to all four so the walk never descends into agent worktrees; the fix + a cache rebuild (rm .spectackle/cache; reindex — the cache is local-only, rebuilds from disk) purges the stale rows permanently. This is the enabling fix that makes worktree-isolated implementers usable without polluting the orchestrator's spec state. Exit criterion (explicit): all four skip-lists include .claude; with a dummy .claude/worktrees/x/.spectackle/spec.md present, ./bin/spectackle lint . still reports only the real 15 files/61 rules (not doubled) and check stays ok; go test ./... -race green. Rollback: revert the four one-line additions. Scope (disjoint): internal/workspace/workspace.go, internal/spec/cascade.go, internal/index/indexer.go, internal/mcpserver/tools.go.

## T-0083 add .claude to the four directory-walk skip-lists
kind: task
state: active
created: 2026-07-24
parent: P-0057

IMPLEMENTER IN A DEDICATED WORKTREE; NO lifecycle moves (orchestrator owns them); only lease claim, code+test, lease release, report. Scope (disjoint): internal/workspace/workspace.go, internal/spec/cascade.go, internal/index/indexer.go, internal/mcpserver/tools.go. Edits (add the string '.claude' to each skip-list, matching existing style): (1) workspace.go — the walk switch `case ".git", "node_modules", "testdata":` (around line 203, inside ContextDirs) becomes `case ".git", "node_modules", "testdata", ".claude":`. (2) cascade.go — the same `case ".git", "node_modules", "testdata":` (around line 85) becomes `case ".git", "node_modules", "testdata", ".claude":`. (3) indexer.go — the ignoreDirs map literal (around line 86) gains `".claude": true,` (add to the map). (4) tools.go — the coverageGaps walk switch (around line 1056) that lists `case ".git", "node_modules", "testdata", "bin", ".spectackle":` gains `, ".claude"`. Nothing else. Test: create a throwaway nested bundle to prove the skip works — in a tempdir-based test OR manually: mkdir -p .claude/worktrees/x/.spectackle && cp .spectackle/spec.md .claude/worktrees/x/.spectackle/spec.md, then ./bin/spectackle lint . must still report 15 spec files / 61 rules / 0 findings (NOT doubled), then rm -rf .claude/worktrees/x. Also add/extend a Go test if one exists for ContextDirs or cascade discovery asserting a .claude subdir is skipped (check internal/workspace and internal/spec for existing discovery tests; if present, add a case; if not, skip the Go test and rely on the manual proof, noting it). Verify: go test ./internal/workspace/ ./internal/spec/ ./internal/index/ ./internal/mcpserver/ -race; make build; the manual dummy-bundle lint proof. lease release. Report: the four diffs, the lint-not-doubled proof, test results. Rollback: git checkout the four files.
