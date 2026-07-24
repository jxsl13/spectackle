# spectacle

**A token-efficient, spec-driven MCP server for cross-language codebases.**

spectacle gives LLM coding agents (Claude Code, Codex CLI, and any other MCP
client) a complete, git-native spec lifecycle plus cross-language code
intelligence — so large refactors run on flat-rate agent tools instead of
usage-billed APIs:

```
            ┌─────────────────────────────────────────────────────┐
            │                    spectacle (MCP)                  │
 structural │  cross-language AST map      (Aider-inspired)       │
            │  go:Saxpy ─cgo→ c:launch ─launch→ cu:kernel         │
            ├─────────────────────────────────────────────────────┤
topological │  cascading spec bundles      (.spectacle/spec.md)   │
            │  root → module → directory, overrides explicit      │
            ├─────────────────────────────────────────────────────┤
   semantic │  EARS notation, linted       (WHEN …, X SHALL …)    │
            │  vague prose is a build error                       │
            ├─────────────────────────────────────────────────────┤
  lifecycle │  proposals → tasks → done → archived (+ revocable   │
            │  rejections); journal = the learn-from-failure      │
            │  corpus; drift detection closes the loop            │
            └─────────────────────────────────────────────────────┘
```

**The loop** (taught to the LLM by the server's own MCP instructions —
self-bootstrapping, no docs needed):

1. `find scope=rejection` — learn why similar work failed before.
2. `find scope=code` + `get depth=2` — impact radius across Go → C → CUDA/ASM.
3. `draft kind=proposal targets=…` — one round trip returns the **context
   pack**: impact, binding EARS contracts, similar past rejections.
4. On user approval: `move to=approved`, `draft` tasks, `rule op=add`
   contracts (server-composed EARS, lint-gated, auto-ID, elicitation).
5. Implement; `check` until `ok` — drift between code and contracts is
   detected via content-hash anchors and back-propagated as proposals.
6. `move to=done` → `to=archived` — the delta merges into the living spec;
   `compact` folds noise (rejections are never lost).

The LLM **never writes spec files**: everything lives in versioned
`.spectacle/` folders (max three bundle files per context dir — no file
sprawl), written exclusively by the server; the SQLite/FTS5 cache underneath
is local-only and rebuilds from disk (no migrations, pre-v1 by design).

Initial focus: **Go** with arbitrary native bindings (cgo/C, C++, CUDA,
Plan 9 ASM, Objective-C/Metal, Vulkan); parser and resolver layers are
plugin interfaces for arbitrary languages later.

## Status (v0, pre-release — anything may break)

| Component | State |
|---|---|
| EARS linter + cascading spec bundles | ✅ live |
| Spec lifecycle: draft/move/archive, revocable rejections, journal corpus | ✅ live |
| Server-authored contracts (`rule`: slots → compose → lint gate → auto-ID, MCP elicitation) | ✅ live |
| Unified search (`find`) over rules/items/history/rejections (SQLite FTS5, pure Go) | ✅ live |
| Drift anchors + backprop (`check`/`compact`) | ✅ live (spans pending until M1 graph) |
| **Multi-agent swarm**: scope leases, shared coord.db, realtime sibling learnings, git-worktree isolation (`work start/submit/abort`) with semantic replay merge | ✅ live |
| Cross-language graph, tree-sitter/`go/parser` indexing | 🔜 M1/M2 |
| Self-hosting gate | 🔜 M5 ([roadmap](docs/roadmap.md)) |

## Quickstart

```sh
make build                # -> bin/spectacle (pure Go, CGO_ENABLED=0)
./bin/spectacle lint .    # lint all EARS spec bundles
```

Register with Claude Code (or any MCP client):

```json
{
  "mcpServers": {
    "spectacle": {
      "command": "/absolute/path/to/bin/spectacle",
      "args": ["serve"]
    }
  }
}
```

The workspace root is auto-detected (`.spectacle/config.yaml` marker, then
git root, then `-root`).

## Documentation

- [docs/lifecycle.md](docs/lifecycle.md) — **the lifecycle architecture**: storage, search, state machine, compacting, drift/backprop
- [docs/tools.md](docs/tools.md) — the 7 MCP tools: JSON Schemas + output grammar
- [docs/spec-cascade.md](docs/spec-cascade.md) — cascading spec bundles: format, resolution, authoring
- [docs/ears.md](docs/ears.md) — the EARS grammar and linter codes
- [docs/architecture.md](docs/architecture.md) — cross-language AST analysis (parsers, resolvers, graph)
- [docs/example-go-cuda.md](docs/example-go-cuda.md) — worked Go → CUDA lifecycle transcript
- [docs/roadmap.md](docs/roadmap.md) — milestones to self-hosting

## Dogfooding

This repository carries its own contracts in `.spectacle/` bundles;
`go test ./...` fails if any committed spec violates the EARS grammar, and
the `check` tool must come back clean on the repo itself — spectacle is its
own first user.
