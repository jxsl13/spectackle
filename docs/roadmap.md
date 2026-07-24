# Roadmap — to self-hosting (all of this is pre-v1: anything may break)

| M | Theme | Deliverables | Exit criterion |
|---|---|---|---|
| **M0** ✅ | Skeleton + spec engine | Pure-Go skeleton; EARS linter + cascade loader live; server-authored contracts | committed specs lint clean via own binary |
| **M0.5** ✅ | **Full spec lifecycle** | Workspace discovery; bundled `.spectackle/` layout (spec.md/work.md/journal.ndjson); 7-tool orthogonal surface (find/get/draft/rule/move/check/compact); SQLite-FTS5 cache + sync engine; state machine with revocable rejections; drift anchors + backprop; hybrid compacting; self-bootstrapping server instructions | lifecycle E2E tests green; `check` clean on own repo |
| **M0.6** ✅ *(this iteration)* | **Multi-agent swarm** | Shared coord.db (WAL, N stdio processes): agent registry + heartbeats, scope leases, global ID counters, swarm event log with sw-piggyback; git-worktree orchestration (`work start/submit/abort`, code-only commits, integrate lock, semantic .spectackle replay with idempotent retry); `lease`/`work`/`swarm` tools; SPX-SWM contracts | two-server tests green under -race: unique IDs, lease contention, realtime rejection sharing, concurrent submits |
| **M1** ✅ | Structural core | `go/parser`-based Go indexer (no tree-sitter for Go), tree-sitter C parser (cgo bindings), populated graph with `EndLine` spans, degree ranking | `find scope=code`/`get depth` real for Go + cgo edges on this repo; anchors leave `pending` |
| **M2** ✅ *(parse cache + cuda/asm chains live; tree-sitter C/C++ parsers still open)* | Cross-language + parse cache | cpp/cuda parsers; all four resolvers (cgo, plan9asm, cuda, gpupipe-stub); parse-blob cache tables; context pack `#impact` fully real | saxpy transcript in docs/example-go-cuda.md reproduces verbatim |
| **M3** | Spec integration depth | `applies:`-aware `ForNode`; orphan-rule coverage; mergeable-rule compaction candidates; personalized PageRank with `focus` | `check` gates CI on this repo |
| **M4** | Hardening | cursors everywhere; linter fuzzing; perf: ≥100k LOC repo indexed < 5 s warm; optional streamable-HTTP transport | perf target met on a reference repo |
| **M5** | **Self-hosting gate** | CI: lint + check + coverage threshold; CONTRIBUTING documents the loop: every change starts with `find scope=rejection` + `draft` | a spectackle feature is merged that was developed through spectackle's own lifecycle |
| **M6** | Extension | ObjC/Metal/Vulkan resolvers; **wazero/WASM parser backend replaces cgo tree-sitter** (`CGO_ENABLED=0` release builds); new-language cookbook | adding a language touches only `langs.go` + one parser + one resolver |

## Standing constraints

- **v0 semantics**: no release tag yet; formats, tools and schemas may break
  at any time. The `schema: v0` stamp marks the current format — it rotates
  on breakage, caches rebuild, there is no migration mechanism anywhere.
- **cgo-free target**: cgo is a bridge technology for the tree-sitter phase
  (M1–M5); the target binary is pure Go. The current build is already
  `CGO_ENABLED=0` (including SQLite via modernc.org/sqlite).
- **Token efficiency is a feature**: any new tool must specify budget
  behaviour and dense output records before it is added; the tool count
  stays minimal and orthogonal.
- **The LLM never writes .spectackle files** — every lifecycle capability
  ships as a server-side write path, never as a file-format convention.
