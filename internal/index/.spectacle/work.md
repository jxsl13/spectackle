---
schema: v0
---

## P-0012 M3 slice 1: go/types call-edge upgrade pass (two-tier per R-0002)
kind: proposal
state: approved
created: 2026-07-24
targets: go:coord.DB.Sweep, internal/index/indexer.go

Implement the recommended first slice of docs/design-go-types-calls.md: exported index.ResolveTypedCalls(ctx, g, root) using golang.org/x/tools/go/packages (Mode NeedName|NeedTypes|NeedSyntax|NeedTypesInfo), resolving every CallExpr via TypesInfo to *types.Func and adding ECall edges under the existing ID scheme go:<pkg.Name()>[.<recv>].<name> - only where both endpoints already exist as nodes. Synchronous after IndexAll in reindex (resident service absorbs the cost; module-hash caching deferred to slice 2). Exit: Neighbors(go:coord.DB.Sweep, In) contains go:mcpserver.Server.preCall.

## T-0021 implement index.ResolveTypedCalls + tests (no server wiring)
kind: task
state: done
created: 2026-07-24
parent: P-0012

Scope ONLY: new internal/index/typespass.go + typespass_test.go, go.mod/go.sum (go get golang.org/x/tools). ResolveTypedCalls(ctx context.Context, g graph.Graph, root string) (added int, err error): packages.Load{Dir: root, Mode: NeedName|NeedTypes|NeedSyntax|NeedTypesInfo|NeedFiles} patterns ./...; per pkg per syntax file: track enclosing *ast.FuncDecl (src ID minted like GoParser incl. receiver); for each CallExpr resolve callee: SelectorExpr -> pkgs TypesInfo.Selections[sel].Obj() or TypesInfo.Uses[sel.Sel]; Ident -> Uses[ident]; accept only *types.Func with fn.Pkg()!=nil and fn.Pkg().Path() not equal to the fake cgo package; mint dst go:<fn.Pkg().Name()>.<recvTypeName>.<fn.Name()> (recv from fn.Type().(*types.Signature).Recv(), unwrap pointer, drop generics brackets) or without recv part; add ECall edge (File=call site rel path from root, Line) ONLY if g.Node(src) and g.Node(dst) both exist and src!=dst; dedupe per (src,dst); return count. Errors from packages.Load or per-pkg Errors: return err (caller treats non-fatal). Tests: (1) temp module (write go.mod module example.com/m + pkg a with type S{ d *b.D } method Run calling s.d.Sweep() + pkg b type D method Sweep) -> after GoParser IndexAll over the temp root + ResolveTypedCalls, Neighbors(go:a.S.Run, Out, ECall) contains go:b.D.Sweep; (2) repo e2e guarded by testing.Short skip: IndexAll over ../../ then ResolveTypedCalls, assert Neighbors(go:coord.DB.Sweep, In) contains go:mcpserver.Server.preCall; (3) determinism: run twice, same added count... second run adds 0 new because dedupe checks existing? memGraph Upsert appends duplicate edges - so ResolveTypedCalls must dedupe against EXISTING Neighbors(src,Out,ECall) too. go build ./... && go vet ./internal/index/ && go test ./internal/index/ -race green. NO server.go changes (orchestrator wires).
