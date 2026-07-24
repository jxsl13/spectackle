# Design sketch — go/types-based call resolution (M3)

Status: research draft, no implementation. Scope: `callEdges` in
`internal/index/goparser.go` only; node extraction is unaffected.

## 1. Problem

`GoParser.Parse` (§93-125 of `goparser.go`, function `callEdges`) mints
`ECall` edges by syntactic convention, not by resolving identifiers:

```go
switch fn := call.Fun.(type) {
case *ast.Ident:
    dst = graph.NodeID(ids.Mint("go", pkg+"."+fn.Name))
case *ast.SelectorExpr:
    if x, ok := fn.X.(*ast.Ident); ok && x.Name != "C" {
        dst = graph.NodeID(ids.Mint("go", x.Name+"."+fn.Sel.Name))
    }
}
```

Two independent gaps follow directly from this:

1. **Only single-level selectors are visited.** The `*ast.SelectorExpr` case
   requires `fn.X` to be a plain `*ast.Ident`. Any chained selector —
   `s.cd.Sweep()`, `s.db.tx.Commit()` — has `fn.X` typed as another
   `*ast.SelectorExpr`; the switch falls through, `dst` stays `""`, and the
   call is **silently dropped**, not even minted as a dangling edge to prune.
2. **The minted ID uses the identifier's spelling, not its resolved
   meaning.** Even a single-level `x.Sel()` call mints `go:<x>.<Sel>` — the
   literal source token. Method node IDs are `go:<pkg>.<Recv>.<Name>` (three
   components; `Parse`, line 56). A local receiver variable name essentially
   never equals `<pkg>.<Recv>`, so the guess matches nothing and is pruned
   as dangling. Qualified calls to plain functions in another package
   (`pkgalias.Func()`) are the one case that usually *does* resolve, since
   the import alias conventionally equals the package name used to mint IDs.

**Graph evidence.** `internal/mcpserver/swarm.go:51` calls the coordinator
through a chained selector, `s.cd` being a `*coord.DB` field — exactly the
failure mode above:

```go
if _, err := s.cd.Sweep(s.agentTTL()); err != nil {
```

Querying the live graph confirms it — no edges at all, depth 1 or 2:

```
$ get id=go:coord.DB.Sweep depth=1
n go:coord.DB.Sweep method internal/coord/coord.go:208 sig=(agentTTL time.Duration)([]Lease,error)
```

The actual caller, `mcpserver.Server.preCall`, exists as a node but is
simply not linked to `Sweep`:

```
$ find q=preCall scope=code
n go:mcpserver.Server.preCall method internal/mcpserver/swarm.go:42 sig=()error
```

An agent asking "who calls `DB.Sweep`?" gets a false "nobody" today.

## 2. Sketch: `go/packages` + `go/types`

Replace (or augment) the syntactic pass with a semantic one: load the module
with `golang.org/x/tools/go/packages` (`Mode: NeedName|NeedFiles|NeedSyntax|
NeedTypes|NeedTypesInfo|NeedDeps` — not currently a dependency, see go.mod).
For every `*ast.CallExpr`, look up `pkg.TypesInfo.Uses[ident]` (plain call)
or `pkg.TypesInfo.Selections[sel]` (selector call) to get a `*types.Func` via
`Selection.Obj()`, which resolves through embedding and pointer/value
receivers automatically. This is exactly what subsumes the chained-selector
gap: `types.Info` already flattens `s.cd.Sweep` to the concrete
`*coord.DB.Sweep` method regardless of how many field hops sit in between.
Mint the destination ID from the resolved `*types.Func`, not source text:
`go:<pkg>.<recv>.<name>`, `pkg = fn.Pkg().Name()` (mirrors today's
`f.Name.Name` convention, see §3), `recv` from
`fn.Type().(*types.Signature).Recv()` when non-nil.

## 3. Discussion points

**ID stability vs. today's package-name IDs.** IDs are minted from the
declaring file's `package` clause (`f.Name.Name`), which every MCP tool,
cache row, and test fixture already keys on. A types-based resolver must
derive the *same* string — `fn.Pkg().Name()`, not `fn.Pkg().Path()` — or
every node ID becomes unreachable from the new edges. Recommendation: leave
`Parse`'s node-minting loop untouched; only `callEdges` changes its
resolution strategy, still emitting `ids.Mint("go", pkg+"."+recv+"."+name)`
— an additive migration.

**Performance.** `packages.Load` type-checks the whole module transitively,
~10-100x slower than a bare `go/parser.ParseFile` per file — unacceptable as
the steady-state per-file cost in the walk→hash→cache-hit pipeline
(architecture.md §1). Mitigations, increasing complexity: (a) **two-tier
graph** — the fast syntactic pass stays the immediate, always-on `ECall`
producer; `packages.Load` runs as a slower async pass that *upgrades* edges
once per full `IndexAll`, never regressing to zero edges meanwhile; (b)
**scope to `IndexAll` only**, gated behind the same generation-stamp
invalidation the SQLite cache uses (lifecycle.md §2); (c) **cache by module
hash** — reduce `TypesInfo.Uses`/`Selections` to a serializable
`(call-site → callee-ID)` table keyed by a hash of `go.mod`+`go.sum` plus
the changed file set, in `.spectackle/cache/`.

**Interface dispatch.** A call through an interface-typed receiver
(`w.Write(...)`, `w io.Writer`) has no single concrete `*types.Func` — only
the interface method. Resolving to *all* implementers needs a whole-program
`types.Implements` closure over every concrete type in scope, a distinct
and pricier analysis. Defer to M3+; resolve such calls to the interface
method node itself (still better than today's silent drop) for now.

**CGO.** `go/types` treats `import "C"` as a synthetic package with
synthesized types per `C.foo` reference — it does not see real C symbols.
`callEdges`'s existing `x.Name != "C"` exclusion stays as-is; the CGO
boundary remains owned by `resolve.CgoResolver` (architecture.md §3).

## 4. Recommendation and exit criterion

Adopt the two-tier design of §3(a): `go/parser`-only stays the fast,
always-correct-for-nodes path; add a `go/packages`+`go/types` upgrade pass
scoped to `IndexAll`, cached by module hash, emitting the same
`go:<pkg>.<recv>.<name>` ID shape so it composes with existing edges rather
than replacing them. Interface dispatch and cross-module resolution beyond
the repo root are out of scope for the first cut; direct and
embedded-selector method calls (the `s.cd.Sweep()` class of gap) are the
concrete, testable win and the M3 acceptance target.

Once implemented, `get id=go:coord.DB.Sweep depth=1` must show
`go:mcpserver.Server.preCall` as an incoming caller (today: no edges at all,
per §1) — the regression test for this design, without touching node
minting or unrelated edge kinds (`ECgo`, `EAsm`, `ELaunch`).
