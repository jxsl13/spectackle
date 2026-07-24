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

## P-0055 ADRs searchable: implement find scope=decision (documented but missing)
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

The user's point — spectackle should store + make ADRs searchable, not unstructured markdown — has a concrete, already-half-built answer: decisions ARE first-class lifecycle items (kind=decision, D-ids; D-0002/D-0003/D-0004 are literal architecture decision records with question, options, chosen outcome, and the item they block). docs/tools.md §14 explicitly documents 'find scope=decision', but scopeKinds in internal/mcpserver/tools.go (find) omits 'decision', so `find scope=decision` returns '! ARG E - unknown scope decision'. Fix: add decision as a searchable scope so the ADR corpus is queryable exactly like rejections/history/research already are. This aligns the tool with its own docs and directly serves 'make ADRs searchable' without inventing a parallel store. Exit criterion (explicit): scopeKinds maps 'decision' -> {"decision"}; the findIn scope jsonschema enum/description lists decision; find scope=decision returns the D-items (verify the sync/cache actually indexes kind=decision — if a decision isn't returned, report that the indexer needs a decision-kind entry, do NOT fake it); a test asserts find scope=decision finds a drafted decision; docs/tools.md find schema enum includes decision; go test ./internal/mcpserver/ -race green; ./bin/spectackle lint . clean. Rollback: revert the diff. Scope (disjoint): internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, docs/tools.md.

## T-0082 find scope=decision: scopeKinds entry + schema + test
kind: task
state: active
created: 2026-07-24
parent: P-0055

IMPLEMENTER RUNS IN A DEDICATED WORKTREE; does NOT move items (orchestrator owns moves). Only: lease claim, code+test, lease release, report. Scope (disjoint): internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, docs/tools.md. Steps: (1) lease claim item=T-0082 paths=[internal/mcpserver/tools.go, internal/mcpserver/tools_test.go, docs/tools.md]. (2) tools.go: in the scopeKinds map (around line 203) add the entry `"decision": {"decision"},` (keep map formatting). (3) tools.go: the findIn struct's Scope jsonschema tag lists the enum in prose ('code|rule|spec|proposal|task|bug|research|rejection|history|all, default all') — add 'decision' to that list. (4) VERIFY the cache indexes decision items: write a test in tools_test.go named TestFindScopeDecision that connectRoot(t, tempdir), drafts a decision via the decide tool (decide op=ask with a question + options and kind=radio; in a headless/no-elicitation test this creates a submitted D-item — confirm the item exists via state/get), then calls find scope=decision q=<word from the question> and asserts the D-id appears in the output. If the decision item is NOT returned by find even after adding the scope, that means the sync engine (internal/sync or the cache indexer) does not index kind=decision — in that case report EXACTLY that finding and which file needs the decision-kind indexing entry; do not fake a pass. (5) docs/tools.md: in the find tool JSON schema (section 1), add 'decision' to the scope enum array. (6) Verify: go test ./internal/mcpserver/ -run 'TestFind|TestToolSurface' -race; go vet ./internal/mcpserver/; ./bin/spectackle lint . (run inside your worktree). (7) lease release. Report: the diff, test result, and whether decisions are actually returned by find (or the indexing gap if not). Rollback: git checkout the three files.
