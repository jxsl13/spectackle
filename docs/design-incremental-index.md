# Design research — node removal for incremental `IndexPaths` (R-0003)

Status: research, no implementation. Scope: whether `memGraph` needs a
delete/ownership path at all, given `IndexPaths` is currently a documented
no-op (`internal/index/indexer.go`) and `graph.Graph` has no
`RemoveFile`/`Delete` — only additive `Upsert` (`internal/graph/graph.go`).

## 1. Problem

`memGraph.Upsert` only adds/overwrites nodes and *appends* edges; nothing
retracts a node or edge a since-deleted or since-edited file contributed.
`IndexPaths` cannot re-index "just this file" without either undoing that
file's prior contribution first, or falling back to a full `IndexAll`. IDs
complicate removal further: `disambiguate` (`indexer.go`) assigns collision
suffixes (`~2`, `~3`, …) in whole-tree file-path order, so removing one
file's nodes can change which file owns the un-suffixed base ID for
everyone else colliding on it — partial removal is not "delete this file's
rows."

Briefing options: (a) per-file node/edge ownership index +
`RemoveFile(path)` then reparse; (b) generation-stamped nodes with lazy
sweep; (c) keep `IndexAll` as the only path, relying on the existing
content-hash parse cache (`internal/store`). Instruction: measure first —
`IndexAll` is already ~40ms syntactic + a cached typed pass on this repo,
so incrementality may never pay below the M4 gate (≥100k LOC indexed <5s
**warm**).

## 2. Method

Throwaway harness (`cmd/benchtmp`, built and run in a scratch git worktree,
never committed) drives `index.New` exactly as `internal/mcpserver/server.go`
wires it (`GoParser`+`AsmParser`+`CudaParser`, `resolve.Default().All()`),
timing `IndexAll` and `ResolveTypedCalls` separately, cold vs. warm,
against **this repo** (73 `.go` files, 13,689 LOC, real deps —
`golang.org/x/tools`, `modernc.org/sqlite`, the MCP SDK) and **synth1000 /
synth5000**, generated trees (one flat package per 50 files, ~49 LOC/file,
zero deps, each with its own `go.mod` so `ResolveTypedCalls` runs there
too): 1000 files/49,000 LOC and 5000 files/245,000 LOC.

"Cold" = fresh `graph.NewMem()` + fresh store (mem: `store.NewMem()`;
persistent: `store.Open` on an empty DB file). "Warm" = fresh
`graph.NewMem()`, same store handle, unchanged tree — the shape
`indexer_cache_test.go` proves parses zero files. Two full runs (below)
confirm the numbers aren't a fluke.

## 3. Results

| Scenario | mem cold | mem warm | sqlite cold | sqlite warm | typed cold | typed warm (cache hit) |
|---|---|---|---|---|---|---|
| repo (73 files, 13.7k LOC) | 40-42ms | 14-15ms | 340-390ms | 19-20ms | 291ms-2.85s* | 3-5ms |
| synth1000 (1k files, 49k LOC) | 120-126ms | 71-81ms | 3.2-4.3s | 111-117ms | 300-304ms | 15-17ms |
| synth5000 (5k files, 245k LOC) | 580-583ms | 333-383ms | 21.5-24.0s | 591ms-748ms | 1.6-2.3s | 66-73ms |

Each cell is the range across two runs. `nodes`/`edges` counts were
byte-identical cold vs. warm in every scenario (repo: 713/687; synth1000:
6000/1000; synth5000: 30000/5000) — the cache path reproduces the graph
exactly, as `indexer_cache_test.go` already guarantees for a smaller
fixture.

\* repo "typed cold" spans 291ms-2.85s: `ResolveTypedCalls` shells out to
`go/packages`, and the repo's real dependency graph (the synthetic trees
import only `fmt`) makes the *first* invocation in a session pay for a cold
Go build cache; a second process in the same environment sees 291ms — an
artifact of `go build`'s own cache, not of anything this design controls.

Two findings outside R-0003's scope but worth flagging: **sqlite cold
scales ~linearly at ~4.3ms/file** (`store.Put` is one unbatched `db.Exec`
per file, a synchronous commit each) — dwarfs the parse cost at scale, a
plain batching bug unrelated to node removal. And **`ResolveTypedCalls`'s
module-hash key hashes every tracked file** (`typespass.go
moduleHashKey`), so any single file edit invalidates the whole cache and
forces a full `packages.Load` (the cold numbers above, not the 3-73ms warm
one) — a real incrementality gap, but it lives in the typed-call cache key,
not in `memGraph`.

## 4. Break-even analysis

The M4 gate is ≥100k LOC indexed **<5s warm**. synth5000 (245k LOC, already
2.4x the gate) warm-rebuilds in 333-383ms (mem) or 591-748ms (sqlite) —
7-15x under budget, using nothing but the existing content-hash cache and a
full `IndexAll` re-walk. Extrapolating the sqlite-warm slope (~120-150μs/
file — the more realistic backend, since `-http` resident mode persists)
linearly, the warm path needs on the order of **30k-40k files (~1.5M-2M
LOC)** before a full-rebuild `IndexAll` alone risks the 5s budget — 15-20x
past the M4 gate's own reference size, outside any repo this tool targets
today (roadmap.md's reference class is "a repo," not a monorepo that size).

Node removal's payoff is bounded by what it *saves* against this warm
baseline: skipping the walk/hash/lookup and disambiguate+Upsert+rank
rebuild for unchanged files. At current scale that baseline is already
sub-second; removal machinery would shave milliseconds off milliseconds. It
starts to matter only once warm-rebuild cost approaches multi-second — past
the break-even size above, not at the M4 gate.

## 5. Options

**(a) Per-file ownership index + `RemoveFile(path)`.** Track, per file, the
node IDs it owns (including collision-suffixed ones) and edges it
contributed; `RemoveFile` deletes those from `memGraph`, then a normal
reparse+Upsert re-adds the current version. Simplest, most surgical. Real
cost: collision-suffix stability — `disambiguate` assigns `~N` in
whole-tree file order, so removing the file owning a base ID must either
re-run `disambiguate` globally (defeats the point) or leave a "hole" until
the next full `IndexAll` — bounded staleness, not a bug, but needs a test
pinning it (SPX-GRA-001 requires suffix determinism; this adds "and after
a removal").

**(b) Generation-stamped nodes + lazy sweep.** Tag every node/edge with the
generation that last touched it; a changed file's reparse bumps the
counter and re-Upserts; a sweep drops anything stamped stale whose file is
gone. Avoids (a)'s bookkeeping at the cost of a second moving part (the
sweep) and a window where stale nodes are visible — acceptable for a "map,"
questionable for `Impact`/`get` answers an agent edits code from.

**(c) Rebuild-from-cache (status quo).** Keep `IndexPaths` a no-op; every
refresh is a full `IndexAll`, accelerated by the unchanged content-hash
store. Zero new code, zero new invariants beyond SPX-GRA-001/002. Cost is
exactly §3/§4 above.

## 6. Recommendation

Ship nothing for R-0003 now; keep (c). Warm-rebuild cost at 2.4x the M4
target size (245k LOC, 333-748ms) leaves an order of magnitude of headroom
before the 5s budget is at risk, and the break-even point for (a)/(b) sits
15-20x past the M4 gate itself. Building ownership tracking now would be
complexity spent against a problem the numbers say does not exist yet.

**Exit criterion:** once a real target repo's **warm** `IndexAll` (not
cold) measures within 2x of the 5s M4 budget, re-open this design starting
from option (a) — the smaller, more testable unit (one `RemoveFile` entry
point, one ownership map) versus (b)'s sweep scheduler, its one known
hazard (collision-suffix holes, §5) narrow enough to pin with a single
regression test. Until then, the standing gaps worth fixing are the two §3
findings unrelated to node removal: batch `store.Put` writes (fixes the
21-24s sqlite-cold number at scale), and scope `ResolveTypedCalls`'s
module-hash key to the changed file set instead of the whole module (fixes
the 291ms-2.85s single-file-edit cache miss).
