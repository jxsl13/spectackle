---
schema: v0
---

## T-0125 go parser: call edges from closure-var bodies and explicit generic instantiations
kind: task
state: done
created: 2026-07-25
parent: P-0086
refs: R-0005, ADR-0012
targets: internal/index/goparser.go, internal/index/goparser_test.go, internal/index/typespass.go, internal/index/typespass_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Findings: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/go.md; fixture: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-go (21 ground-truth symbols; Multiplier and Run demonstrate both gaps).

TWO CONFIRMED GAPS (nodes are perfect — edges only):
1. Package-level `var F = func(...) {...}` bodies are never scanned for call sites: both the syntactic callEdges walk (goparser.go) and the typed pass (typespass.go) visit only *ast.FuncDecl bodies. Fix: also walk ValueSpec func-literal initializer bodies, attributing edges to the var's node (which already exists).
2. Explicit generic instantiation `Foo[T](args)`: call.Fun is *ast.IndexExpr/*ast.IndexListExpr and both callee switches (syntactic + typed calleeFunc) only case *ast.Ident/*ast.SelectorExpr. Fix: unwrap the index expression to its base and proceed as today (both passes).

TESTS: extend goparser_test.go and typespass_test.go with the two fixture shapes (copy from gap-go/sample.go); assert the edges Multiplier->Add and Run->Sum exist after the fix; assert no behavior change elsewhere (existing tests untouched and green).
VERIFY: go build ./... ; go test ./internal/index/... -race ; go test ./... ; go vet ./internal/index/... ; /home/user/spectackle/bin/spectackle lint. Then the live probe: reindex /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-go and show the two previously-missing e lines via get depth=1.
ROLLBACK: two visitor/switch extensions, revertible. REPORT: real test output, the live e lines, anything deliberately not done.

## T-0126 asm + cuda parsers: linker-suffix symbol forms and kernel modifier orders
kind: task
state: done
created: 2026-07-25
parent: P-0086
refs: R-0005, ADR-0012
targets: internal/index/asmparser.go, internal/index/asmparser_test.go, internal/index/cudaparser.go, internal/index/cudaparser_test.go

IMPLEMENTER IN OWN WORKTREE. Read this whole body first. Findings: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/asm.md and /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/findings/cuda.md; fixtures: /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-asm, /tmp/claude-0/-home-user-spectackle/0a4152cf-4ac0-54d5-b7bf-fee7e0bff69c/scratchpad/gap-cuda.

ASM (5 high): TEXT/GLOBL regexes miss (a) file-local symbols with the <> static suffix and no middle dot (TEXT shuffle<>(SB), GLOBL mask<>(SB)); (b) ABI-tagged symbols (TEXT ·addAVX2<ABIInternal>(SB)); (c) quoted method-shaped linker symbols (TEXT "".Vector.Add(SB)). Extend the symbol-name patterns; strip the <...> suffix from the minted name, keep the middle-dot handling as is.
CUDA (6 high per findings): static-qualified __global__ kernels and the other confirmed modifier-order forms in the findings file mint no node — extend the kernel/device patterns accordingly.
TESTS: one case per fixed form in each parser's _test.go, sourced from the fixtures. Existing tests stay green unmodified.
VERIFY: go build ./... ; go test ./internal/index/... -race ; go test ./... ; go vet ./internal/index/... ; /home/user/spectackle/bin/spectackle lint. Live probe: reindex both fixture dirs, show before/after node counts.
ROLLBACK: pattern-level additions, revertible. REPORT: per parser, forms fixed + real counts, anything deliberately not done.
