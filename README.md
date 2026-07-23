# spectacle

**A token-efficient, spec-driven MCP server for cross-language codebases.**

spectacle gives LLM coding agents (Claude Code, Codex CLI, and any other MCP
client) three fused capabilities that together minimize context tokens and
rework loops — so you can run large refactors on flat-rate agent tools instead
of usage-billed APIs:

```
            ┌─────────────────────────────────────────────────────┐
            │                    spectacle (MCP)                  │
            │                                                     │
 structural │  cross-language AST map      (Aider-inspired)       │
            │  go:Saxpy ─cgo→ c:launch ─launch→ cu:kernel         │
            ├─────────────────────────────────────────────────────┤
topological │  cascading spec files        (.spectacle.ears.md)   │
            │  global → module → directory, overrides explicit    │
            ├─────────────────────────────────────────────────────┤
   semantic │  EARS notation, linted       (WHEN …, X SHALL …)    │
            │  vague prose is a build error                       │
            └─────────────────────────────────────────────────────┘
```

**The synergy loop** (one composite tool call, `plan_change`):

1. The agent names a change target; the **AST map** computes the impact
   radius across language boundaries (Go → C → CUDA/ASM/Metal/Vulkan).
2. The server auto-loads the **cascading EARS contracts** for exactly that
   radius — never the whole spec corpus.
3. The agent translates the strict EARS conditions deterministically into
   each affected language and edits with full contract knowledge —
   **zero full-file reads before editing.**

New contracts are never hand-written: the `add_rule` tool composes them from
structured slots (elicited from the end user through the MCP client when
needed), lints them, and assigns IDs — spec files are server-managed
artifacts that humans review in git diffs.

Initial focus: **Go** projects with arbitrary native bindings (cgo/C, C++,
CUDA, Plan 9 ASM, Objective-C/Metal, Vulkan). The parser and resolver layers
are plugin interfaces designed for arbitrary languages later.

## Status (M0)

| Component | State |
|---|---|
| EARS linter (6 patterns, codes E001–E006/W001–W002) | ✅ working |
| Cascading spec loader (inherit/override/scope) | ✅ working |
| Guided contract authoring (`add_rule`: slots → compose → lint gate → auto-ID, MCP elicitation) | ✅ working |
| MCP server, 11 tools over stdio | ✅ serving; spec tools live |
| Cross-language graph, tree-sitter/`go/parser` indexing | 🔜 M1/M2 (interfaces final, stubs return `stub milestone=M1`) |
| Self-hosting (spectacle developed via spectacle) | 🔜 M5 gate, specs already in place |

See [docs/roadmap.md](docs/roadmap.md).

## Quickstart

```sh
make build          # -> bin/spectacle (pure Go, CGO_ENABLED=0 works)
./bin/spectacle lint .    # lint all EARS spec files
```

Register with Claude Code (or any MCP client):

```json
{
  "mcpServers": {
    "spectacle": {
      "command": "/absolute/path/to/bin/spectacle",
      "args": ["serve", "-root", "/absolute/path/to/your/repo"]
    }
  }
}
```

## Documentation

- [docs/architecture.md](docs/architecture.md) — indexing pipeline, binding resolvers, graph model, ranking, cache
- [docs/spec-cascade.md](docs/spec-cascade.md) — cascading spec files: naming, front matter, resolution
- [docs/ears.md](docs/ears.md) — the EARS grammar and linter codes
- [docs/tools.md](docs/tools.md) — MCP tool schemas and the dense output line grammar
- [docs/example-go-cuda.md](docs/example-go-cuda.md) — worked Go → CUDA example with tool transcript
- [docs/roadmap.md](docs/roadmap.md) — milestones M0 → M6 (self-hosting)

## Dogfooding

This repository carries its own contracts: `.spectacle/global.ears.md` plus
scoped `.spectacle.ears.md` files next to each subsystem. `go test ./...`
fails if any committed spec violates the EARS grammar — spectacle is its own
first user.
