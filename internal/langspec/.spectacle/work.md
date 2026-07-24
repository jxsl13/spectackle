---
schema: v0
---

## P-0023 langspec wave 2: kotlin, swift, c#, scala, shell, lua, zig, perl — 20 languages total
kind: proposal
state: approved
created: 2026-07-24
grilled: 2026-07-24
targets: internal/langspec/langspec.go, go:langspec.All

Global goal: encapsulated support for as many languages as possible. Wave 1 proved the pattern (a language is ONE Spec data value, init()-registered, zero shared-file wiring); wave 2 adds the eight most common remaining regex-parseable languages in three disjoint batches:
Batch D — kotlin (.kt/.kts: fun incl. modifiers, class/object/interface/enum class -> KType), swift (.swift: func incl. modifiers, class/struct/enum/protocol/extension -> KType).
Batch E — csharp (.cs: java-style method heuristic with leading modifier keyword requirement; class/interface/struct/record/enum -> KType), scala (.scala: def -> KFunc, class/object/trait/case class -> KType).
Batch F — shell (.sh/.bash: `name() {` and `function name` -> KFunc), lua (.lua: `function name(`, `local function name(`, `function M.name(` -> KFunc), zig (.zig: `fn`/`pub fn` -> KFunc, `const X = struct/enum/union` -> KType), perl (.pl/.pm: `sub name` -> KFunc, `package Name` -> KType).
Each batch = one fresh implementer, files strictly disjoint (one lang per file + its test). Orchestrator wires afterwards: graph.go Lang consts (kt/swift/cs/scala/sh/lua/zig/pl) + index/langs.go extLang entries (.kt .kts .swift .cs .scala .sh .bash .lua .zig .pl .pm); implementers use graph.Lang string literals matching those exact values.
Rollback: additive files only. Exit: go build/vet, go test -race ./internal/langspec/, make all; reindex on this repo stays green.

## T-0041 langspec batch D: kotlin + swift
kind: task
state: active
created: 2026-07-24
parent: P-0023

SCOPE (only these four files, all new): internal/langspec/kotlin.go, kotlin_test.go, swift.go, swift_test.go. Reference pattern: internal/langspec/python.go (+_test) and rust.go — one Spec value per file, `func init() { registry = append(registry, xSpec) }`.

kotlinSpec: Lang graph.Lang("kt"), Exts [".kt", ".kts"], QualFileStem. Defs:
- KFunc: `^\s*(?:(?:public|private|protected|internal|open|override|suspend|inline|operator|infix|tailrec|external|abstract|final)\s+)*fun\s+(?:<[^>]*>\s+)?(\w+)` — plain/modifier/suspend/generic funs; name capture after optional generic params.
- KType: `^\s*(?:(?:public|private|protected|internal|open|abstract|final|sealed|data|inner|value)\s+)*(?:class|object|interface)\s+(\w+)` — covers `enum class`/`data class` via the modifier list (enum add to modifiers: include `enum` and `annotation` in the modifier alternation).

swiftSpec: Lang graph.Lang("swift"), Exts [".swift"], QualFileStem. Defs:
- KFunc: `^\s*(?:(?:public|private|fileprivate|internal|open|static|class|override|final|mutating|nonmutating|convenience|required|@\w+)\s+)*func\s+(\w+)`
- KType: `^\s*(?:(?:public|private|fileprivate|internal|open|final|indirect|@\w+)\s+)*(?:class|struct|enum|protocol|actor|extension)\s+(\w+)`
- KFunc init: `^\s*(?:(?:public|private|fileprivate|internal|required|convenience)\s+)*(init)\s*\(` — Swift initializers, captured as literal name `init`.

TESTS (SPX-TST-001, mirror rust_test.go structure): fixture-based positives for EVERY Def line (expect exact IDs like kt:<stem>.<name> and line numbers), negatives (kotlin: `fun` inside a comment, a call site `myFun(x)`, `if` lines; swift: closure `{ (x: Int) in`, call `foo()`, property decl), determinism (parse twice, identical output), registry test (All() contains the spec; graph.Lang("kt")/("swift") reachable).

CONSTRAINTS: graph.Lang string literals "kt"/"swift" exactly (orchestrator adds LangKt/LangSwift consts + extLang after); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.

EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ all green.

## T-0042 langspec batch E: c# + scala
kind: task
state: active
created: 2026-07-24
parent: P-0023

SCOPE (only these four files, all new): internal/langspec/csharp.go, csharp_test.go, scala.go, scala_test.go. Reference pattern: internal/langspec/python.go and ESPECIALLY java.go — the C# method heuristic mirrors javaSpec's (leading-modifier requirement excludes fields/control flow without an exclusion list).

csharpSpec: Lang graph.Lang("cs"), Exts [".cs"], QualFileStem. Defs:
- KType: `^\s*(?:(?:public|private|protected|internal|abstract|sealed|static|partial|readonly|ref)\s+)*(?:class|interface|struct|record|enum)\s+(\w+)`
- KFunc (method heuristic, java.go style): indented line, >=1 modifier keyword (public|private|protected|internal|static|virtual|override|abstract|sealed|async|extern|partial|new), then a return-type token ([\w<>\[\],.?]+), then name, then `(`... optional `where` clauses, ending `{` or `=>` or `;` (interface/abstract). Copy java.go's regex shape and adapt: allow expression-bodied `=> ...;` endings and generic methods `Name<T>(`.

scalaSpec: Lang graph.Lang("scala"), Exts [".scala"], QualFileStem. Defs:
- KFunc: `^\s*(?:(?:private|protected|override|final|implicit|inline|lazy)\s+)*def\s+(\w+)`
- KType: `^\s*(?:(?:private|protected|abstract|final|sealed|implicit|case)\s+)*(?:class|object|trait|enum)\s+(\w+)` — `case class`/`case object` via the modifier alternation.

TESTS (SPX-TST-001, mirror java_test.go structure): fixture positives for every Def (exact IDs cs:<stem>.<name> / scala:<stem>.<name> + line numbers) — C# fixture MUST include: a field decl, an `if`/`return` inside a method (all non-matching), a generic method, an expression-bodied method, an interface method decl; scala fixture: case class, object, trait, plain def, an `if` line + a call line as negatives. Determinism + registry tests like rust_test.go.

CONSTRAINTS: graph.Lang string literals "cs"/"scala" exactly (orchestrator adds consts + extLang after); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.

EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ all green.

## T-0043 langspec batch F: shell + lua + zig + perl
kind: task
state: active
created: 2026-07-24
parent: P-0023

SCOPE (only these eight files, all new): internal/langspec/shell.go, shell_test.go, lua.go, lua_test.go, zig.go, zig_test.go, perl.go, perl_test.go. Reference pattern: internal/langspec/python.go (+_test) — one Spec value per file, `func init() { registry = append(registry, xSpec) }`.

shellSpec: Lang graph.Lang("sh"), Exts [".sh", ".bash"], QualFileStem. Defs (both KFunc):
- `^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{?` — POSIX `name() {`
- `^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{` — bash `function name {` (no parens)
Negatives to prove: command invocations (`echo foo`), `if [ ... ]`, subshell `(cd x)`, case patterns `foo)`.

luaSpec: Lang graph.Lang("lua"), Exts [".lua"], QualFileStem. Defs (KFunc):
- `^\s*(?:local\s+)?function\s+([A-Za-z_][A-Za-z0-9_.:]*)\s*\(` — plain, local, `M.name`, `Obj:method` (dots/colons stay in the captured name; ids.Mint accepts any non-space qualifier)
- `^\s*(?:local\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s*=\s*function\s*\(` — `name = function(...)`.
Negatives: `function` in a comment `-- function foo`, a call `foo(1)`, `end` lines.

zigSpec: Lang graph.Lang("zig"), Exts [".zig"], QualFileStem. Defs:
- KFunc: `^\s*(?:pub\s+)?(?:export\s+)?(?:extern\s+(?:"\w+"\s+)?)?(?:inline\s+)?fn\s+(\w+)`
- KType: `^\s*(?:pub\s+)?const\s+(\w+)\s*=\s*(?:packed\s+|extern\s+)?(?:struct|enum|union|opaque)\b`
Negatives: `const x = 5;`, fn type in a param `comptime f: fn () void` (mid-line, anchored ^ excludes), call lines.

perlSpec: Lang graph.Lang("pl"), Exts [".pl", ".pm"], QualFileStem. Defs:
- KFunc: `^\s*sub\s+([A-Za-z_][A-Za-z0-9_]*)`
- KType: `^\s*package\s+([A-Za-z_][A-Za-z0-9_:]*)` — `::` stays in the name.
Negatives: anonymous `my $f = sub {`, comment `# sub foo`, calls.

TESTS (SPX-TST-001, mirror rust_test.go structure per language): fixture positives with exact IDs (sh:<stem>.<name>, lua:..., zig:..., pl:...) and line numbers, the listed negatives, determinism, registry membership via All().

CONSTRAINTS: graph.Lang string literals "sh"/"lua"/"zig"/"pl" exactly (orchestrator adds consts + extLang after); do NOT touch langspec.go, any existing language file, graph/, index/, or .spectacle/ (server-owned). Never commit/push.

EXIT CRITERION: go build ./... && go vet ./... && go test -race ./internal/langspec/ all green.
