---
schema: v1
prefix: SPX
---

## SPX-LSP-001 {applies: go:langspec.All}
The langspec registry SHALL define a language purely as one `Spec` data value so adding a language never modifies the indexing pipeline.
## intent
- P-01KY9SCNFREE5A6QCMAWK99MRX langspec wave 2: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total: wave 2 shipped: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total, all batches green under -race, one self-caught fix (C# indentation guard vs record primary constructors)
- P-01KYA07R7GFVBT5YBWXB5DJEBP langspec wave 3: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total: wave 3 shipped: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total; OCaml proves Def.Name multi-group capture, R proves uppercase-extension routing e2e
- P-01KYA0YFRREKC91G2YD2JREHH9 langspec call edges + C/C++ FFI resolution — validated on ddnet: shipped in four slices: CallRe framework (LSP-001), Allman spans (506->30627 edges), cspan leaf package + FFI fix (+4453 cpp->c bridges, str_copy 612 cpp callers on ddnet); honest residual limits documented in docs/validation-ddnet.md (inline ctors, attribute-prefixed defs)
- T-01KYCG4YA0F83T7T0WVXZDGC3W end-terminated languages onto EndSpan: lua, ruby, elixir, julia, fortran — spans, call edges, Def fixes: lua, ruby, elixir, julia and fortran on EndSpan with real body spans and call edges; ruby operator and setter methods, julia macros and fortran derived types now mint
- T-01KYCG4Z98E7XTCRZWAZDZ8DW7 langspec hardening jvm/dotnet: java, kotlin, scala, groovy, csharp: java, kotlin, scala, groovy and csharp hardened; all five gained call edges, csharp recall 9 to 22 symbols on the fixture
- T-01KYCG4Z98FKKAFDN2V6JA0GQP langspec hardening c-family/shader: c, cpp, objc, metal, glsl: c, cpp, objc, metal and glsl hardened; cpp 11 to 21 symbols, glsl gained 8 edges, objc phantom duplicate declarations removed
- T-01KYCG4Z98FH3VGA3D75BM88NJ langspec hardening systems/functional: rust, swift, zig, haskell, ocaml, erlang: rust, swift, zig, haskell, ocaml and erlang hardened; rust, swift and zig gained call edges, ocaml recall 11 to 22 symbols
- T-01KYCG4YA0F18VSJ2C5ZNC7DCK langspec hardening web: javascript (+NEW test), typescript, dart: web hardening live: javascript 10 to 20 nodes with 7 edges (class methods, constructors and getters minted no node at all before), typescript 10 to 23 with 7 edges (class and interface members), dart 13 to 24 with 5 edges; all three gained CallRe so spans bound real bodies. Pre-existing tests that asserted EndLine==Line encoded the span-collapse defect and were corrected.
- T-01KYCG4Z98EN9AGR7YY0MXZHWB langspec hardening scripting: python (+NEW test), perl, php, shell, r: scripting hardening live: python 18 to 21 nodes (async def at any indentation, nested classes, Sig capture) with its first dedicated test file including a pinned rationale for leaving CallRe nil; perl, php, shell and r gained CallRe so all four emit call edges for the first time (5, 6, 10 and 2 on the fixtures) and bound real body spans; r recall 11 to 16 of 19, the remainder low-severity.
- B-01KYCQY9SREEMS0KEQQJQ6XPC5 langspec Parse gates body spans and call edges on KFunc/KMethod only, so kernel-originating calls (KKernel) never mint edges: fixed in T-01KYCT7KG8FD5VG9XKC2SQ37TD: kernel-originating call edges now mint, proven on the metal fixture; the test that pinned their absence as a documented gap became the positive regression

## LSP-001 {applies: go:langspec.SpecParser.Parse}
WHEN a `Spec` sets `CallRe`, the SpecParser SHALL emit `ECall` edges from each Def's brace-counted body span, callee IDs minted in the same language — destinations may be dangling exactly like Go's syntactic pass.

## LSP-002 {applies: go:langspec.SpecParser.callEdges}
WHEN CallRe captures a callee in a QualFileStem language, the SpecParser SHALL mint the callee destination with the same file-stem qualification as definitions, leaving QualFlat callee minting byte-identical.