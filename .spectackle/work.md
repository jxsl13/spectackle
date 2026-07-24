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
state: submitted
created: 2026-07-24

kind: radio
option: keep-deferred: langspec covers 30 languages approximately; wazero buys full C/C++ ASTs at a real binary-size/latency cost R-0004 measured as not worth it — leave as documented, not silent
option: reopen-poc: mint a fresh wazero C-PoC task (parity oracle vs the C langspec chain, binary+latency budget) to re-measure now that langspec is far larger — decide on data
option: commit-full: build the wazero/WASM parser backend now as the M6 target regardless of the earlier measurement
