---
schema: v0
prefix: SPX
---

## SPX-LSP-001 {applies: go:langspec.All}
The langspec registry SHALL define a language purely as one `Spec` data value so adding a language never modifies the indexing pipeline.
## intent
- P-0023 langspec wave 2: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total: wave 2 shipped: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total, all batches green under -race, one self-caught fix (C# indentation guard vs record primary constructors)
- P-0025 langspec wave 3: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total: wave 3 shipped: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total; OCaml proves Def.Name multi-group capture, R proves uppercase-extension routing e2e
- P-0026 langspec call edges + C/C++ FFI resolution — validated on ddnet: shipped in four slices: CallRe framework (LSP-001), Allman spans (506->30627 edges), cspan leaf package + FFI fix (+4453 cpp->c bridges, str_copy 612 cpp callers on ddnet); honest residual limits documented in docs/validation-ddnet.md (inline ctors, attribute-prefixed defs)
- T-0119 end-terminated languages onto EndSpan: lua, ruby, elixir, julia, fortran — spans, call edges, Def fixes: lua, ruby, elixir, julia and fortran on EndSpan with real body spans and call edges; ruby operator and setter methods, julia macros and fortran derived types now mint
- T-0121 langspec hardening jvm/dotnet: java, kotlin, scala, groovy, csharp: java, kotlin, scala, groovy and csharp hardened; all five gained call edges, csharp recall 9 to 22 symbols on the fixture
- T-0122 langspec hardening c-family/shader: c, cpp, objc, metal, glsl: c, cpp, objc, metal and glsl hardened; cpp 11 to 21 symbols, glsl gained 8 edges, objc phantom duplicate declarations removed
- T-0124 langspec hardening systems/functional: rust, swift, zig, haskell, ocaml, erlang: rust, swift, zig, haskell, ocaml and erlang hardened; rust, swift and zig gained call edges, ocaml recall 11 to 22 symbols

## LSP-001 {applies: go:langspec.SpecParser.Parse}
WHEN a `Spec` sets `CallRe`, the SpecParser SHALL emit `ECall` edges from each Def's brace-counted body span, callee IDs minted in the same language — destinations may be dangling exactly like Go's syntactic pass.

## LSP-002 {applies: go:langspec.SpecParser.callEdges}
WHEN CallRe captures a callee in a QualFileStem language, the SpecParser SHALL mint the callee destination with the same file-stem qualification as definitions, leaving QualFlat callee minting byte-identical.