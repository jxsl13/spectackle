---
schema: v0
prefix: SPX
---

## SPX-LSP-001 {applies: go:langspec.All}
The langspec registry SHALL define a language purely as one `Spec` data value so adding a language never modifies the indexing pipeline.

## intent
- P-0023 langspec wave 2: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total: wave 2 shipped: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total, all batches green under -race, one self-caught fix (C# indentation guard vs record primary constructors)
- P-0025 langspec wave 3: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total: wave 3 shipped: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total; OCaml proves Def.Name multi-group capture, R proves uppercase-extension routing e2e

## LSP-001 {applies: go:langspec.SpecParser.Parse}
WHEN a `Spec` sets `CallRe`, the SpecParser SHALL emit `ECall` edges from each Def's brace-counted body span, callee IDs minted in the same language — destinations may be dangling exactly like Go's syntactic pass.
