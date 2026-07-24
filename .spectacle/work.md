---
schema: v0
---

## T-0019 docs refresh: status table, saxpy transcript footnote, roadmap ticks
kind: task
state: done
created: 2026-07-24

Scope ONLY: README.md, docs/example-go-cuda.md, docs/roadmap.md. (1) README status table: flip 'Cross-language graph, tree-sitter/go/parser indexing | M1/M2' row to live (go/parser + plan9 + cuda line-scanner chains, persistent parse cache; tree-sitter/wazero stays future for full C/C++); drift row: remove '(spans pending until M1 graph)'. (2) docs/example-go-cuda.md: closing italic footnote says graph records go live with M1 - delete or rewrite: n/e records and anchor spans ARE live; verify the transcript record shapes still match reality (r-root exists now; #contracts shows dir rules full text; do NOT rewrite the whole transcript, just fix the footnote + any now-false claim). (3) docs/roadmap.md: mark M1 done and M2 partially (parse cache + cuda/asm chains live; tree-sitter C/C++ parsers still open) - tick style like existing checkmarks, one honest parenthetical. No other content edits.

## T-0020 gofmt rest: sync.go, sync_test.go, workspace.go
kind: task
state: done
created: 2026-07-24

Scope ONLY: gofmt -w internal/sync/sync.go internal/sync/sync_test.go internal/workspace/workspace.go. Afterwards gofmt -l internal/ cmd/ MUST be empty. go vet ./... and go test ./internal/sync/ ./internal/workspace/ -count=1 green. Whitespace only, no semantic edits.

## R-0002 design sketch: go/types-based call resolution (M3)
kind: research
state: done
created: 2026-07-24

Scope ONLY: new file docs/design-go-types-calls.md. Analysis, NO implementation. Current limitation (document precisely): GoParser emits convention-based call edges only for ident() and x.Sel() where x is a plain ident != C; chained selectors (s.cd.Sweep()), method values, interface dispatch and cross-package resolution via import paths are missed or guessed (go:x.Sel uses the IDENTIFIER x, not the package path). Sketch: replace/augment with golang.org/x/tools/go/packages + go/types: load packages (Mode NeedTypes|NeedSyntax|NeedTypesInfo), use TypesInfo.Uses/Selections to resolve each call to its *types.Func -> stable ID go:<pkgpath-tail>.<recv>.<name>; discuss: ID stability vs current pkg-name IDs (migration: keep pkg-name minting, derive from types.Func.Pkg().Name()), performance (packages.Load is 10-100x slower than parser - mitigation: only on IndexAll of Go lang, cache by module hash, or a two-tier graph: fast parse pass + async types pass upgrading edges), interface dispatch (types.Implements closure - defer to M3+), CGO (types sees C as fake package - keep resolver as-is). End with a recommendation section + exit criterion (get depth on go:coord.DB.Sweep shows its real callers e.g. mcpserver.Server.preCall). 80-120 lines, tone/format like docs/architecture.md.
