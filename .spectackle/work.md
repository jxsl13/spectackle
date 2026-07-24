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

## P-0054 wazero re-measurement (D-0004 reopen-poc): refresh the tax numbers, decide on data
kind: proposal
state: active
created: 2026-07-24
grilled: 2026-07-24

D-0004 answered reopen-poc: re-measure the wazero/WASM tree-sitter path now that langspec spans 30 languages and cleared the M4 perf gate 7-15x. The PoC harness already exists under poc/wasmparse (its own go.mod, never touches spectackle's): cmd/sizewith + cmd/sizewithout (binary-size-with/without-wazero), cmd/poc (parse harness), corpus/, tswasm/, cparser/, triple/. Task: build+run the harness, capture the current wazero binary-size tax (sizewith - sizewithout, in bytes/MB) and, if the wasm grammar under tswasm/ is present+runnable offline, the parse latency + parity-vs-cparser over corpus/; if a grammar .wasm is missing/unfetchable in this sandbox, record THAT as the finding (no network fabrication). Append the numbers to docs/design-wasm-parsers.md as a new '## Re-measurement (D-0004, reopen-poc)' section; the orchestrator reads the numbers and writes the recommendation. Exit criterion (explicit): the two size binaries build and their byte sizes + delta are recorded; cmd/poc either runs (latency+parity captured) or its blocker is documented; docs/design-wasm-parsers.md has the new section with real measured numbers; ./bin/spectackle lint . clean (the doc is outside spec bundles so lint is unaffected). Rollback: revert docs/design-wasm-parsers.md; poc/ changes are additive/scratch. Scope (disjoint): poc/wasmparse/**, docs/design-wasm-parsers.md.

## T-0081 build+run poc/wasmparse, record wazero tax + parity, write findings section
kind: task
state: active
created: 2026-07-24
parent: P-0054

IMPLEMENTER RUNS IN A DEDICATED WORKTREE and does NOT move lifecycle items (the orchestrator owns all moves per docs/agent-workflow.md 'Worktree isolation & who writes lifecycle state'). You only: lease claim, build+measure, write the findings doc, lease release, then REPORT — do not call move. Scope (disjoint): poc/wasmparse/** and docs/design-wasm-parsers.md. Steps: (1) lease claim item=T-0081 paths=[poc/wasmparse, docs/design-wasm-parsers.md]. (2) cd poc/wasmparse (separate go.mod). Build the size probes: go build -o /tmp/sizewith ./cmd/sizewith and go build -o /tmp/sizewithout ./cmd/sizewithout (read their source first to confirm what each imports; sizewith pulls wazero, sizewithout doesn't). Record ls -l byte sizes of both and compute the delta (the wazero tax) in bytes and MB. (3) Inspect cmd/poc/main.go, corpus/, tswasm/: if a tree-sitter .wasm grammar is present under tswasm/ and cmd/poc runs offline, run it over corpus/ and capture parse latency (wall time) and parity vs cparser (matching/mismatching node counts). If the grammar .wasm is absent or requires network, DO NOT fetch — record 'grammar wasm unavailable offline' as the finding and report only the size tax. (4) Append to docs/design-wasm-parsers.md a new section exactly titled '## Re-measurement (D-0004, reopen-poc)' with a dated bullet list: sizewithout bytes, sizewith bytes, delta (bytes + MB), and either the latency/parity numbers or the documented blocker. Plain measured facts only, no recommendation (the orchestrator writes that). (5) Verify: both size binaries exist and sizes are recorded; go vet ./... inside poc/wasmparse is clean; the doc section exists. (6) lease release. Report the raw numbers verbatim in your final message. Rollback: git checkout docs/design-wasm-parsers.md; poc build artifacts are in /tmp.
