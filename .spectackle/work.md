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
state: active
created: 2026-07-24
grilled: 2026-07-24

User requirement (structured, decided): architecture decisions must be first-class structured records in spectackle, named ADR (industry term), defined+searched via the MCP, never unstructured docs markdown. Scope decided by user: FULL migration of existing records AND FULL ADR fields. Four coupled parts: (1) RENAME kind decision->adr and ID letter D->ADR everywhere: internal/item/item.go (kinds map 'decision':'D' -> 'adr':'ADR'; IDRe '^[PTBRD]-\d{4}$' -> '^(?:ADR|[PTBR])-\d{4}$' to allow the multi-char prefix; any letter-parsing helpers), internal/lifecycle/lifecycle.go (Draft(...,'decision',...) -> 'adr'; comments), the draft-tool kind enum + decide tool (mints kind adr, ID ADR-NNNN), state/find rendering ('i <id> adr ...'), and the server instructions/prompts wording. (2) FULL ADR FIELDS: extend the item model + work.md block serialization with structured Context, Decision, Consequences, Status (classic ADR template) as first-class fields; the decide tool's ask/answer captures Context+options and records Decision+Consequences+Status on answer; get/state/find render them. (3) SEARCHABLE: find scope=adr in scopeKinds + findIn schema, AND the sync indexer must feed kind=adr into FTS (internal/sync feedWork — this was the real gap T-0082 found for decision; carry it forward for adr), so ADR bodies incl. context/consequences are queryable. (4) MIGRATE: rewrite existing D-0002/D-0003/D-0004 in root .spectackle/work.md + journal.ndjson to ADR-0002/03/04 (one-time pre-v1 migration of this repo's own dogfood data), reindex, verify check ok + find scope=adr returns them; and migrate the wazero design rationale from docs/design-wasm-parsers.md into ADR-0004's structured Context/Decision/Consequences (the doc's Recommendation section becomes the ADR, leaving the measurement appendix). Exit criterion (explicit): draft kind=adr mints ADR-NNNN with Context/Decision/Consequences/Status; decide ask/answer populates them; find scope=adr returns ADRs by body text incl. consequences; the three legacy items are ADR-000x and check is ok; go test ./... -race green; ./bin/spectackle lint . clean; docs/tools.md decide+find sections document adr/ADR and the fields. Rollback: revert diff; migration is idempotent string rewrite. Scope: internal/item/**, internal/lifecycle/lifecycle.go, internal/mcpserver/** (decide/find/draft/state/tools + tests), internal/sync/sync.go, docs/tools.md, docs/design-wasm-parsers.md, root .spectackle/ (migration). Large + coupled: staged as tasks; orchestrator owns moves + verifies the migration.

## T-0084 ADR rename part 1: decision->adr, D->ADR, find scope=adr + indexer
kind: task
state: active
created: 2026-07-24
parent: P-0056

IMPLEMENTER IN A DEDICATED WORKTREE; NO lifecycle moves (orchestrator owns them); only lease claim, code+test, lease release, report. This is the RENAME + SEARCHABLE foundation; ADR fields (Context/Decision/Consequences/Status) and data migration are SEPARATE follow-up tasks — do NOT do them here. Scope (disjoint): internal/item/item.go, internal/lifecycle/lifecycle.go, internal/mcpserver/decide.go, internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, internal/mcpserver/decide_test.go (if exists), internal/sync/sync.go, docs/tools.md. Steps: (1) lease claim item=T-0084 with those paths. (2) item.go: in the kinds->ID-letter map change '"decision": "D"' to '"adr": "ADR"'; change IDRe from `^[PTBRD]-\d{4}$` to `^(?:ADR|[PTBR])-\d{4}$` (multi-char ADR prefix alongside single-letter P/T/B/R); update the '// IDRe matches ... D-0007 (decision)' comment to '... ADR-0007 (adr)'. Search item.go for any other 'decision'/'D-' literal and rename to 'adr'/'ADR'. (3) lifecycle.go: change Draft(ws, mint, "decision", ...) (Escalate, ~line 300) to "adr"; rename 'decision item' wording in comments to 'ADR'. (4) decide.go: wherever it drafts/creates the item kind (grep for "decision"), change to "adr" so decide mints ADR-NNNN; update any 'D-' rendering. (5) tools.go: scopeKinds map add '"adr": {"adr"},' (keep 'decision' OUT — full rename, no alias); findIn Scope jsonschema enum/prose: replace any 'decision' with 'adr' and ensure 'adr' is listed. Also the draftIn kind jsonschema if it enumerates kinds — add/rename to include adr if decisions are draftable there. (6) sync.go: in feedWork's indexed-kinds list (the gap T-0082 found — grep for the kinds slice ~line 55), add "adr" so ADR items are FTS-indexed (this is REQUIRED or find scope=adr returns nothing). (7) Server instructions/prompts: grep internal/mcpserver for user-facing 'decision'/'D-xxxx' wording in the instructions const + prompts.go and rename to ADR (keep it accurate). (8) docs/tools.md: the decide section (14) and find schema (1) — rename decision->adr, D->ADR, find scope enum gains adr. (9) Tests: add TestFindScopeADR (mirror the pattern: draft/create an adr item, find scope=adr q=<word>, assert the ADR-id appears) and update any existing test referencing D-/decision/scope=decision. VERIFY EMPIRICALLY that find scope=adr returns a created ADR (the indexer step is essential). Verify: go test ./... -race (FULL suite — the rename touches many packages; all must pass); go vet ./...; make build; ./bin/spectackle lint .; and a live round-trip via the driver in your worktree: decide op=ask ... creates an ADR-NNNN (or need-record), then find scope=adr finds it. (10) lease release. Report: the full diff summary, the live find scope=adr round-trip output, full-suite result. Rollback: git checkout the touched files. NOTE: existing D-0002/03/04 in .spectackle are NOT migrated here (separate task) — after this rename they may show as unrecognized D- ids; that is expected and the migration task fixes it. Do NOT edit .spectackle files.
