---
schema: v0
---

## P-0025 langspec wave 3: dart, groovy, elixir, erlang, julia, haskell, ocaml, r — 28 languages total
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: go:langspec.All

Continues the global goal after wave 2 (20 langs). Eight more regex-parseable languages, three disjoint batches, same proven pattern (one Spec data value per file, init()-registered, zero shared-file wiring):
Batch G — dart (.dart: KType class/mixin/enum/extension with abstract/final/sealed/base/interface modifiers; KFunc return-type+name+parens+{ heuristic requiring >=2-space indent OR top-level void/Future forms, plus `void main()`), groovy (.groovy: KFunc `def name(` and java-style modifier methods; KType class/interface/trait/enum).
Batch H — elixir (.ex/.exs: KFunc def/defp/defmacro name; KType defmodule Name with dotted names kept), erlang (.erl/.hrl: KFunc `name(args) ->` at column 0 only; KType `-module(name).`), julia (.jl: KFunc `function name(` incl. dotted, short-form `name(x) = ` at column 0; KType struct/mutable struct/abstract type).
Batch I — haskell (.hs: KFunc top-level `name ::` type signatures at column 0; KType data/newtype/type/class Name; module Name ignored or KType — implementer picks and documents), ocaml (.ml/.mli: KFunc `let name`/`let rec name` at column 0; KType `type name`, `module Name`), r (.R/.r: KFunc `name <- function(` and `name = function(`).
Orchestrator wires first (consts kt-style + extLang: .dart .groovy .ex .exs .erl .hrl .jl .hs .ml .mli .r — note extLang lowercases unknown-case extensions so .R routes via the .r entry); implementers use graph.Lang literals dart/groovy/ex/erl/jl/hs/ml/r.
Rollback: additive files only. EXIT CRITERION: go build/vet clean, go test -race ./internal/langspec/ green, make all green, reindex of this repo stays green.

## T-0045 langspec batch G: dart + groovy
kind: task
state: active
created: 2026-07-24
parent: P-0025

SCOPE (only these four new files): internal/langspec/dart.go, dart_test.go, groovy.go, groovy_test.go. Reference pattern: python.go (+test), java.go (method heuristic), kotlin.go.

dartSpec: Lang graph.Lang("dart"), Exts [".dart"], QualFileStem. Defs:
- KType: `^\s*(?:(?:abstract|final|sealed|base|interface|mixin)\s+)*(?:class|mixin|enum|extension)\s+(\w+)`
- KFunc top-level: `^(?:void|Future<[^>]*>|Stream<[^>]*>|[A-Z]\w*(?:<[^>]*>)?|int|double|bool|String|num|dynamic)\??\s+(\w+)\s*\([^)]*\)\s*(?:async\*?|sync\*)?\s*\{` — column-0 functions incl. `void main()`.
- KFunc method: same shape but prefixed `^\s{2,}(?:static\s+|@override\s+)*` (>=2-space indent, java.go rationale: prevents top-level type/record collisions).

groovySpec: Lang graph.Lang("groovy"), Exts [".groovy"], QualFileStem. Defs:
- KFunc: `^\s*(?:(?:public|private|protected|static|final|synchronized)\s+)*def\s+(\w+)\s*\(`
- KFunc typed method: `^\s{2,}(?:(?:public|private|protected|static|final|synchronized)\s+)+[\w<>\[\],.]+\s+(\w+)\s*\([^)]*\)\s*\{\s*$` (java.go shape)
- KType: `^\s*(?:(?:public|abstract|final)\s+)*(?:class|interface|trait|enum)\s+(\w+)`

TESTS (SPX-TST-001, mirror rust_test.go): positives with exact IDs (dart:<stem>.<name>, groovy:...) + line numbers — dart fixture MUST include `void main() {`, a generic return type `Future<int> load() async {`, a class method at 2-space indent, an extension; groovy fixture: def function, typed method, trait. Negatives — dart: `if (x) {`, a constructor call line `final a = Foo();`, an import; groovy: `println foo()`, `if` lines. Determinism + registry membership.

CONSTRAINTS: graph.Lang literals "dart"/"groovy" exactly (consts + extLang already wired by orchestrator); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ green.

## T-0046 langspec batch H: elixir + erlang + julia
kind: task
state: active
created: 2026-07-24
parent: P-0025

SCOPE (only these six new files): internal/langspec/elixir.go, elixir_test.go, erlang.go, erlang_test.go, julia.go, julia_test.go. Reference pattern: python.go (+test), rust.go.

elixirSpec: Lang graph.Lang("ex"), Exts [".ex", ".exs"], QualFileStem. Defs:
- KFunc: `^\s*def(?:p|macro|macrop)?\s+([a-z_]\w*[?!]?)` — def/defp/defmacro/defmacrop; trailing ?/! allowed (ids.Mint accepts them).
- KType: `^\s*defmodule\s+([A-Z][\w.]*)` — dotted module names kept verbatim.
Negatives: `# def commented`, a pipe line `|> def_something`, `defstruct [...]` (must NOT match the def regex — note defstruct starts with 'defs', ensure the regex boundary handles it: def(?:p|macro|macrop)? must be followed by whitespace, so defstruct fails).

erlangSpec: Lang graph.Lang("erl"), Exts [".erl", ".hrl"], QualFileStem. Defs:
- KFunc: `^([a-z]\w*)\(.*\)\s*->` — clause heads at column 0 ONLY (Erlang bodies are indented; this dedupes multi-clause functions imperfectly — acceptable, deterministic: each clause head line mints, disambiguation adds ~N suffixes; document this in the file comment).
- KType: `^-module\((\w+)\)` — the module attribute.
Negatives: indented case-clause `  foo(X) ->` (column-0 anchor excludes), `-export([...])`, comments `% foo() ->`.

juliaSpec: Lang graph.Lang("jl"), Exts [".jl"], QualFileStem. Defs:
- KFunc: `^\s*function\s+([\w.!]+)\s*\(` — incl. dotted `Base.show` and bang names.
- KFunc short-form: `^([\w!]+)\s*\([^)]*\)\s*=\s*[^=]` — column 0, name(args) = expr; the [^=] guard excludes `==` comparisons.
- KType: `^\s*(?:mutable\s+)?struct\s+(\w+)` and `^\s*abstract\s+type\s+(\w+)` as TWO Defs.
Negatives: `x = 5`, `if foo(x) == bar(y)`, indented short-form (excluded by column-0), `end`.

TESTS (SPX-TST-001, mirror rust_test.go per language): positives with exact IDs (ex:<stem>.<name> incl. a dotted module, erl:..., jl:...) + line numbers, the negative rosters above, determinism, registry membership.

CONSTRAINTS: graph.Lang literals "ex"/"erl"/"jl" exactly (consts + extLang already wired); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ green.

## T-0047 langspec batch I: haskell + ocaml + r
kind: task
state: approved
created: 2026-07-24
parent: P-0025

SCOPE (only these six new files): internal/langspec/haskell.go, haskell_test.go, ocaml.go, ocaml_test.go, r.go, r_test.go. Reference pattern: python.go (+test), rust.go.

haskellSpec: Lang graph.Lang("hs"), Exts [".hs"], QualFileStem. Defs:
- KFunc: `^([a-z_]\w*'?)\s*::` — column-0 type signatures (the canonical def marker; equation lines without signatures are intentionally not minted — document this tradeoff in the file comment).
- KType: `^(?:data|newtype|type)\s+(\w+)` and `^class\s+(?:[\w() ,=>]+=>\s*)?(\w+)` as TWO Defs (second captures the class name after an optional context `(Eq a) =>`).
Negatives: indented `  helper :: Int -> Int` (where-block, column-0 anchor excludes), `-- foo ::` comment, `instance Show Foo` (instances are not defs).

ocamlSpec: Lang graph.Lang("ml"), Exts [".ml", ".mli"], QualFileStem. Defs:
- KFunc: `^let\s+(?:rec\s+)?([a-z_]\w*'?)` — column-0 let/let rec (nested indented lets excluded).
- KType: `^type\s+(?:('\w+|\([^)]*\))\s+)?([a-z_]\w*)` — capture group for the TYPE NAME is group 2 (type params precede the name in OCaml: `type 'a tree`); verify langspec Def supports selecting capture group 2 — if the Def struct only uses group 1, write the regex with a non-capturing param group: `^type\s+(?:'\w+\s+|\([^)]*\)\s+)?([a-z_]\w*)`.
- KType module: `^module\s+(?:type\s+)?([A-Z]\w*)`.
Negatives: `  let inner = ...` (indented), `(* let commented *)`, `let () = print` (unit pattern — `()` is not \w, no match).

rSpec: Lang graph.Lang("r"), Exts [".r"], QualFileStem — NOTE: only ".r" lowercase in Exts; index.LangOf lowercases unknown-case extensions so .R files route here via extLang; langspec's own extension matching may be case-sensitive — CHECK internal/langspec/langspec.go Extensions()/matching behavior FIRST and, if it is case-sensitive there, include BOTH ".r" and ".R" in Exts (document which path you took in the file comment).
Defs (both KFunc):
- `^\s*([\w.]+)\s*<-\s*function\s*\(` — dot-names common in R (`my.func`).
- `^\s*([\w.]+)\s*=\s*function\s*\(`
Negatives: `x <- 5`, `lapply(v, function(i) i)` (anonymous, mid-line), `# f <- function()` comment.

TESTS (SPX-TST-001, mirror rust_test.go per language): positives with exact IDs (hs:..., ml:... incl. a `type 'a tree` param case proving the name captures correctly, r:... incl. a dot-name) + line numbers, the negative rosters, determinism, registry membership. For R additionally: a fixture parsed via the full indexer path (index.New + IndexAll over a temp dir with a .R uppercase file) proving uppercase routing works end-to-end.

CONSTRAINTS: graph.Lang literals "hs"/"ml"/"r" exactly (consts + extLang already wired); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.
EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ green.
