# Roadmap — M0 → self-hosting

| M | Theme | Deliverables | Exit criterion |
|---|---|---|---|
| **M0** | Skeleton + spec engine *(this iteration)* | Compilable pure-Go skeleton; EARS linter + cascade loader **live**; guided contract authoring (`add_rule` slots → compose → lint gate → auto-ID, with MCP elicitation) **live**; 11 MCP tools over stdio (spec tools real, graph tools stubbed); bootstrap `.spectacle` tree; saxpy example | `make all` green; committed specs lint clean |
| **M1** | Structural core | `go/parser`-based Go indexer (no tree-sitter for Go), tree-sitter C parser (cgo bindings), populated memgraph, degree ranking | `sym`/`map`/`impact` real for Go + cgo edges on this repo |
| **M2** | Cross-language + cache | cpp/cuda parsers; all four resolvers (cgo, plan9asm, cuda, gpupipe-stub); bbolt cache at `.spectacle/cache.db`; `plan_change` fully real | saxpy transcript in docs/example-go-cuda.md reproduces verbatim |
| **M3** | Spec integration | `applies:`/links.tsv resolution in `ForNode`; coverage graph half (uncovered exported symbols, orphan rules); personalized PageRank with `focus` | `coverage` gates CI on this repo |
| **M4** | Hardening | cursors everywhere; linter fuzzing; perf: ≥100k LOC repo indexed < 5 s warm; optional streamable-HTTP transport | perf target met on a reference repo |
| **M5** | **Self-hosting gate** | CI: `spectacle lint .` + coverage threshold (≥ 80 % of exported symbols under contract); CONTRIBUTING documents the workflow: every change starts with `plan_change` | a spectacle feature is merged that was developed through spectacle |
| **M6** | Extension | ObjC/Metal/Vulkan resolvers hardened; **wazero/WASM parser backend replaces cgo tree-sitter** (`CGO_ENABLED=0` release builds); new-language cookbook | adding a language touches only `langs.go` + one parser + one resolver |

## Standing constraints

- **cgo-free target**: cgo is a bridge technology for the tree-sitter phase
  (M1–M5); the target binary is pure Go (Go via stdlib `go/parser`, ASM via
  scanner, C-family via WASM grammars on wazero). See architecture §2.
- **Token efficiency is a feature, not an optimization**: any new tool must
  specify budget behaviour and dense output records before it is added.
- **Dogfooding is the test**: from M5 on, spectacle development itself is the
  acceptance suite — the bootstrap specs in this repo are the seed.
