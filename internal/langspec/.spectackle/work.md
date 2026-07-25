---
schema: v0
---

## T-0119 end-terminated languages onto EndSpan: lua, ruby, elixir, julia, fortran — spans, call edges, Def fixes
kind: task
state: done
created: 2026-07-25
parent: P-0084
refs: R-0005, ADR-0012
targets: internal/langspec/lua.go, internal/langspec/ruby.go, internal/langspec/elixir.go, internal/langspec/julia.go, internal/langspec/fortran.go, internal/langspec/lua_test.go, internal/langspec/ruby_test.go, internal/langspec/elixir_test.go, internal/langspec/julia_test.go, internal/langspec/fortran_test.go

BLOCKED-ON: the EndSpan engine task under P-0084 must be merged first (check via get on this item's parent or find scope=code for KeywordSpan before starting; if absent, stop and report).

For each of lua, ruby, elixir, julia, fortran: set Spec.EndSpan with language-correct open/close regexes (lua: function/if/for/while/do vs end; ruby: def/class/module/do/if/unless/case/begin vs end, beware modifier-if false opens — document your handling; elixir: do-suffixed openers vs end; julia: function/macro/if/for/while/begin/struct vs end; fortran: function/subroutine/module/type vs the matching end-forms, case-insensitive), set CallRe + Stop (language keywords), and fix the [high]/[medium] Def misses from the findings (julia macro definitions, fortran derived types, ruby/elixir specifics per their files).
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.

## T-0120 langspec hardening web: javascript (+NEW test), typescript, dart
kind: task
state: approved
created: 2026-07-25
parent: P-0085
refs: R-0005, ADR-0012
targets: internal/langspec/javascript.go, internal/langspec/typescript.go, internal/langspec/dart.go, internal/langspec/javascript_test.go, internal/langspec/typescript_test.go, internal/langspec/dart_test.go

FAMILY NOTES: javascript has NO existing _test.go — create it. Top misses: JS/TS class instance/static/async methods and getters mint no node at all (no method Def exists); TS 10/23, JS 10/19 recall. dart: arrow-bodied (=>) members.

Languages owned (lease exactly their .go + _test.go files): javascript, typescript, dart. Do NOT set EndSpan (a P-0084 task owns that mechanism and different files) — brace-style fixes only here.
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.

## T-0121 langspec hardening jvm/dotnet: java, kotlin, scala, groovy, csharp
kind: task
state: done
created: 2026-07-25
parent: P-0085
refs: R-0005, ADR-0012
targets: internal/langspec/java.go, internal/langspec/kotlin.go, internal/langspec/scala.go, internal/langspec/groovy.go, internal/langspec/csharp.go, internal/langspec/java_test.go, internal/langspec/kotlin_test.go, internal/langspec/scala_test.go, internal/langspec/groovy_test.go, internal/langspec/csharp_test.go

FAMILY NOTES: Top misses: java constructors (no return type); kotlin extension-function receiver mis-captured as the name; scala 3 extension methods; groovy default-visibility methods; csharp Allman-brace block bodies (cspan handles Allman since T-0053 — fix the def-line regex, not the engine).

Languages owned (lease exactly their .go + _test.go files): java, kotlin, scala, groovy, csharp. Do NOT set EndSpan (a P-0084 task owns that mechanism and different files) — brace-style fixes only here.
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.

## T-0122 langspec hardening c-family/shader: c, cpp, objc, metal, glsl
kind: task
state: approved
created: 2026-07-25
parent: P-0085
refs: R-0005, ADR-0012
targets: internal/langspec/c.go, internal/langspec/cpp.go, internal/langspec/objc.go, internal/langspec/metal.go, internal/langspec/glsl.go, internal/langspec/c_test.go, internal/langspec/cpp_test.go, internal/langspec/objc_test.go, internal/langspec/metal_test.go, internal/langspec/glsl_test.go

FAMILY NOTES: Top misses: c same-line bodies; cpp inline class-body methods; objc @protocol; metal multi-line kernel signatures (first line lacks the closing paren — cspan multi-line headers exist, fix the def regex); glsl structs.

Languages owned (lease exactly their .go + _test.go files): c, cpp, objc, metal, glsl. Do NOT set EndSpan (a P-0084 task owns that mechanism and different files) — brace-style fixes only here.
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.

## T-0123 langspec hardening scripting: python (+NEW test), perl, php, shell, r
kind: task
state: approved
created: 2026-07-25
parent: P-0085
refs: R-0005, ADR-0012
targets: internal/langspec/python.go, internal/langspec/perl.go, internal/langspec/php.go, internal/langspec/shell.go, internal/langspec/r.go, internal/langspec/python_test.go, internal/langspec/perl_test.go, internal/langspec/php_test.go, internal/langspec/shell_test.go, internal/langspec/r_test.go

FAMILY NOTES: python has NO existing _test.go — create it. Top misses: python async def; perl/php/shell/r leave CallRe nil despite brace bodies — set CallRe + Stop and verify edges appear (r: nested/anonymous function assignment forms per findings).

Languages owned (lease exactly their .go + _test.go files): python, perl, php, shell, r. Do NOT set EndSpan (a P-0084 task owns that mechanism and different files) — brace-style fixes only here.
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.

## T-0124 langspec hardening systems/functional: rust, swift, zig, haskell, ocaml, erlang
kind: task
state: approved
created: 2026-07-25
parent: P-0085
refs: R-0005, ADR-0012
targets: internal/langspec/rust.go, internal/langspec/swift.go, internal/langspec/zig.go, internal/langspec/haskell.go, internal/langspec/ocaml.go, internal/langspec/erlang.go, internal/langspec/rust_test.go, internal/langspec/swift_test.go, internal/langspec/zig_test.go, internal/langspec/haskell_test.go, internal/langspec/ocaml_test.go, internal/langspec/erlang_test.go

FAMILY NOTES: Top misses: rust const fn modifier order; swift override init; zig error-set types; haskell multi-line type signatures; ocaml and-chained lets; erlang guard-clause heads.

Languages owned (lease exactly their .go + _test.go files): rust, swift, zig, haskell, ocaml, erlang. Do NOT set EndSpan (a P-0084 task owns that mechanism and different files) — brace-style fixes only here.
IMPLEMENTER IN OWN WORKTREE. Read this whole body first; do not explore beyond the files named here plus the read-only inputs below.

READ-ONLY INPUTS (ground truth from R-0005, outside the repo — read, never modify):
  findings per language: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/<lang>.md  (the empirically confirmed misses: construct, severity, example, edges verdict)
  scratch fixtures:      /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang>/         (the exact sample workspaces whose symbols were the ground truth; reuse their code as regression-test input, copied into your Go tests as string literals or testdata — do not reference /tmp paths from committed tests)

METHOD per language: read the findings file; for every [high] and [medium] miss, extend or add the pattern so the construct mints the right node/edge; encode each fixed construct as a test case in the language's _test.go (copy the relevant fixture lines in); do not chase [low] items unless free. Preserve existing behavior: every pre-existing test stays green unmodified unless a finding proves the expectation itself wrong (justify in the report if so).

VERIFY (run all, real output): go build ./... ; go test ./internal/langspec/... -race ; go test ./... ; go vet ./internal/langspec/... ; /home/user/spectackle/bin/spectackle lint. Then re-run the empirical probe for each of your languages: /home/user/spectackle/bin/spectackle reindex -root /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-<lang> and confirm via find scope=code (call -root that dir) that previously-missed symbols now appear; paste the before/after node counts.

ROLLBACK: per-file regex/data changes, individually revertible. REPORT BACK: per language, the constructs fixed, node count before/after over the fixture, and anything you deliberately did NOT do.
