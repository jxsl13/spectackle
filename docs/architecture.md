# Architecture — cross-language AST analysis

## 1. Pipeline

```
walk root ─→ hash (sha256) ─→ cache hit? ──yes──→ load ParseResult from store
   │                              │no
   │                              └→ LanguageParser.Parse ─→ nodes + same-lang edges
   └→ after all files:  BindingResolver pass ─→ cross-language edges
                        └→ graph.Upsert  ─→ rank ─→ serve MCP tools
```

- **Walk**: honors `.spectacle/config.yaml` `ignore` globs; skips `.git`.
- **Parse**: per-file, parallel workers; a single writer goroutine owns
  `graph.Upsert` (see §7).
- **Resolve**: runs after parsing because binding edges need symbols from
  *both* sides of a language boundary.
- **Determinism**: identical file bytes ⇒ identical nodes/edges/IDs
  (SPX-GRA-001). All ordering is by file path, then line.

## 2. Parser backends — and the cgo-free target picture

Parsing is behind one interface (`internal/index.LanguageParser`); backends
are swappable per language without touching graph, resolvers, or tools:

| Language | Backend (initial) | Backend (target) | Why |
|---|---|---|---|
| Go | stdlib `go/parser` (+ `go/types` for call resolution) | same | pure Go, better fidelity than any grammar |
| Plan 9 ASM | custom line scanner (`internal/index/plan9asm.go`) | same | no viable tree-sitter grammar for Plan 9 syntax (`TEXT ·name(SB)`, pseudo-registers, middle dot); GAS/NASM grammars don't fit |
| C | tree-sitter `tree-sitter-c` via cgo bindings | tree-sitter compiled to **WASM, run via wazero** | see below |
| C++ / Metal MSL | tree-sitter `tree-sitter-cpp` (MSL is C++14-derived) | WASM/wazero | " |
| CUDA | `tree-sitter-grammars/tree-sitter-cuda` (C++ superset: `<<<>>>`, `__global__`) | WASM/wazero | " |
| Objective-C | tree-sitter objc grammar | WASM/wazero | " |

**cgo strategy.** tree-sitter is a C library; the official Go bindings
(`github.com/tree-sitter/go-tree-sitter` v0.25.0) use cgo. That is acceptable
for a locally-run binary, so M1/M2 start there. The *target* is a cgo-free
binary (`CGO_ENABLED=0`): tree-sitter grammars compile to WASM (the
web-tree-sitter path proves this works) and run on
[wazero](https://wazero.io), a pure-Go WASM runtime — the same approach the
wasilibs projects use for C libraries. Migration is invisible to the rest of
the system because only `LanguageParser` implementations change. The M0
skeleton is already cgo-free.

Verified module pins for the cgo phase (all fetchable as Go modules):
`tree-sitter/go-tree-sitter v0.25.0`, `tree-sitter/tree-sitter-go v0.25.0`
(unused — Go uses stdlib), `tree-sitter/tree-sitter-c v0.24.2`,
`tree-sitter/tree-sitter-cpp v0.23.4`,
`tree-sitter-grammars/tree-sitter-cuda v0.21.1`.

**Symbol extraction** uses per-grammar tags queries (`.scm` files, the Aider
approach): one query per grammar captures definition and reference nodes;
adding a language = grammar + tags query + extension mapping.

## 3. Binding resolvers (the cross-language edge factory)

`internal/resolve.BindingResolver` is the extension point: each resolver
detects one FFI boundary and emits edges. Built-ins and their detection
contracts (doc comments in the source are normative):

- **cgo** (`ECgo`): `import "C"` preamble → `#include` edges; `C.<ident>`
  selector references → edge from enclosing Go func to `c:<ident>`
  (unresolved C symbols become stub nodes so the radius stays complete);
  `//export Name` → reverse edge (C calls Go).
- **plan9asm** (`EAsm`): bodyless `func Name(...)` in `.go` ↔
  `TEXT ·Name(SB)` in a sibling `.s` (any GOARCH suffix).
- **cuda** (`ELaunch`): `__global__` defs become `cu:` kernel nodes;
  `name<<<grid,block>>>(…)` / `cudaLaunchKernel` → launch edge. `extern "C"`
  wrappers in `.cu` files are minted as `c:` symbols, so the chain
  `go:X ─cgo→ c:wrapper ─launch→ cu:kernel` composes from two independent
  resolvers.
- **gpupipe** (`ELaunch`, best-effort): Metal `newFunctionWithName:`/
  `makeFunction(name:)` and Vulkan `VkPipelineShaderStageCreateInfo.pName`
  string literals matched against shader entry points of the same name;
  heuristic edges carry a rank penalty so symbol-resolved paths outrank them.

A new language integrates by adding: extension mapping (`index/langs.go`),
a `LanguageParser`, and (if it has FFI surfaces) a `BindingResolver`.

## 4. Graph model

- **Node** `{ID, Kind, Lang, File, Line, Sig, Rank}` — kinds:
  `fn method type var kernel asm file dir`.
- **NodeID** `"<lang>:<qualified-name>"` (`go:saxpy.Saxpy`,
  `cu:saxpy_kernel`), collisions disambiguated deterministically with `~2`,
  `~3`… in file-path order (`internal/ids`). IDs are the token currency of
  every MCP tool: the LLM names code by ID, never by content.
- **Edge** `{Src, Dst, Kind, File, Line}` — kinds:
  `def call incl cgo asm launch use link`. `File:Line` locate the *site*
  (call/launch statement), which is exactly where an agent needs to edit.
- **Storage**: queries always run against the in-memory graph
  (`graph.NewMem`); persistence is only a parse cache (§6).

## 5. Ranking & token budgeting

- **Rank**: degree-based centrality in M1; personalized PageRank in M3, with
  `map.focus` seeding the personalization vector (the Aider trick — the map
  reshapes itself around what the agent is working on).
- **Budget**: every map/graph/contract tool takes `budget` (tokens,
  ~4 bytes/token estimate). Results are emitted as ranked line records;
  `budget.TruncateRecords` cuts at record boundaries and appends a
  `cur <cursor>` record (SPX-ARC-002). Cursors are opaque base64 offsets.

## 6. Incremental cache

The lifecycle cache already lives in `.spectacle/cache/index.db` (pure-Go
SQLite via modernc.org/sqlite, FTS5; gitignored, generation-stamped, rebuilt
on mismatch — see docs/lifecycle.md §2). The M1/M2 indexer adds parse-blob,
node and edge tables to the same DB (`internal/store.Store` is the interim
in-memory blob interface). Invalidation: file hash changes ⇒ reparse that
file, then rerun only the resolvers whose `Langs()` intersect the changed
language set.

## 7. Concurrency

Parse workers fan out per file (results are pure functions of file bytes);
one writer applies `Upsert` batches; readers take an RWMutex. Tool calls are
read-only except the lifecycle write paths (`draft`, `rule`, `move`,
`compact`), which are confined to `.spectacle/` folders.

## 8. Server surface

`internal/mcpserver` uses the **official MCP Go SDK**
(`github.com/modelcontextprotocol/go-sdk` v1.6.1): typed tool handlers,
schemas inferred from Go structs, stdio transport (`mcp.StdioTransport`).
Streamable HTTP can be added later without touching tool code. stdout is
reserved for JSON-RPC frames; all logging goes to stderr (SPX-ARC-001).
