package index

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
)

// ResolveTypedCalls is the go/types-based call-edge upgrade pass (M3 slice
// 1, docs/design-go-types-calls.md). It loads and type-checks the Go module
// rooted at root with golang.org/x/tools/go/packages, resolves every
// *ast.CallExpr's callee through *types.Info (which flattens chained and
// embedded selectors — s.cd.Sweep() — to the concrete method regardless of
// how many field hops sit in between), and adds any ECall edge the fast
// syntactic pass in GoParser.callEdges missed or mis-minted.
//
// This is a two-tier upgrade, not a replacement (design doc §4): it never
// removes or duplicates edges the syntactic pass already produced. It is
// intended to run once per IndexAll, not per file change — packages.Load
// type-checks the whole module and is 10-100x slower than a bare
// go/parser.ParseFile per file.
//
// An edge is added only when both endpoints already exist as nodes in g
// (mirroring the syntactic pass's dangling-edge pruning), the edge is not a
// self-loop, and it is not already present — either newly discovered earlier
// in this same pass, or already an ECall edge out of the same source node
// from a prior run (memGraph.Upsert appends rather than replaces, so callers
// running ResolveTypedCalls repeatedly must not re-add the same edge).
//
// ID minting mirrors GoParser/ids exactly: go:<pkg>.<recv>.<name> (recv
// omitted for a plain function), pkg = fn.Pkg().Name() — the declaring
// package's source name, not its import path — so resolved edges land on
// the very same node IDs the syntactic pass and every other tool already
// key on.
//
// A call resolving into the cgo pseudo-package (import "C") is skipped: that
// boundary is owned by resolve.CgoResolver. In practice cmd/cgo rewrites
// C.foo(...) call sites to a wrapped identifier before go/types ever sees
// them, so this rarely fires; it is kept as a defensive, explicit guard
// rather than relying on that incidentally.
//
// Interface-typed call sites resolve to the interface method node itself
// (not to every implementer) — whole-program Implements-closure resolution
// is out of scope for this slice (design doc §3, "Interface dispatch").
//
// packages.Load failures (a broken module, a bad build) and any non-empty
// Package.Errors are returned as errors; the caller (the IndexAll
// orchestrator) treats a failed upgrade pass as non-fatal to the syntactic
// index it already has.
func ResolveTypedCalls(ctx context.Context, g graph.Graph, root string) (int, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     root,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return 0, fmt.Errorf("typespass: load %s: %w", root, err)
	}
	var loadErrs []string
	for _, p := range pkgs {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, e.Error())
		}
	}
	if len(loadErrs) > 0 {
		return 0, fmt.Errorf("typespass: %d package error(s) loading %s, e.g. %s", len(loadErrs), root, loadErrs[0])
	}

	// existing[src] lazily caches the set of dst IDs already reachable via an
	// ECall edge out of src, so a repeated call to ResolveTypedCalls over an
	// already-upgraded graph adds nothing (determinism, see doc comment).
	existing := map[graph.NodeID]map[graph.NodeID]bool{}
	hasEdge := func(src, dst graph.NodeID) bool {
		m, ok := existing[src]
		if !ok {
			m = map[graph.NodeID]bool{}
			for _, e := range g.Neighbors(src, graph.Out, []graph.EdgeKind{graph.ECall}) {
				m[e.Dst] = true
			}
			existing[src] = m
		}
		return m[dst]
	}

	type pair struct{ src, dst graph.NodeID }
	seen := map[pair]bool{}
	var newEdges []graph.Edge

	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			abs := p.Fset.Position(f.Package).Filename
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				// out-of-tree / synthetic file (e.g. a cgo preprocessing temp
				// file whose //line directives point outside root): nothing
				// in it can be a node GoParser minted, skip it.
				continue
			}
			rel = filepath.ToSlash(rel)
			for _, e := range typedCallEdges(p.Fset, p.TypesInfo, f, rel) {
				if _, ok := g.Node(e.Src); !ok {
					continue
				}
				if _, ok := g.Node(e.Dst); !ok {
					continue
				}
				if e.Src == e.Dst {
					continue
				}
				key := pair{e.Src, e.Dst}
				if seen[key] || hasEdge(e.Src, e.Dst) {
					continue
				}
				seen[key] = true
				newEdges = append(newEdges, e)
			}
		}
	}

	if len(newEdges) > 0 {
		g.Upsert(nil, newEdges)
	}
	return len(newEdges), nil
}

// typedCallEdges walks one type-checked file's top-level func/method
// declarations and emits a candidate ECall edge for every CallExpr whose
// callee resolves to a *types.Func. Candidates are not yet filtered against
// the graph or deduped against prior runs — the caller does that once, using
// the same pair across every file so a call site's duplicate within one decl
// still collapses to a single edge (mirrors GoParser.callEdges' per-src
// `seen` set).
func typedCallEdges(fset *token.FileSet, info *types.Info, f *ast.File, rel string) []graph.Edge {
	var edges []graph.Edge
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Body == nil {
			continue
		}
		obj, _ := info.Defs[d.Name].(*types.Func)
		if obj == nil {
			continue
		}
		src := funcID(obj)
		if src == "" {
			continue
		}
		seen := map[graph.NodeID]bool{}
		ast.Inspect(d.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := calleeFunc(info, call)
			if fn == nil || fn.Pkg() == nil || isCgoPkg(fn.Pkg()) {
				return true
			}
			dst := funcID(fn)
			if dst == "" || dst == src || seen[dst] {
				return true
			}
			seen[dst] = true
			edges = append(edges, graph.Edge{
				Src: src, Dst: dst, Kind: graph.ECall,
				File: rel, Line: fset.Position(call.Pos()).Line,
			})
			return true
		})
	}
	return edges
}

// calleeFunc resolves a CallExpr's callee to the *types.Func it denotes, or
// nil if it isn't a (possibly indirected) reference to exactly one function
// or method. A selector call resolves through Selections first — that map
// already flattens embedding and pointer/value receivers to the concrete
// method regardless of how many field hops the source spells out (s.cd.Sweep
// resolves the same as cd.Sweep would) — and falls back to Uses for a
// qualified identifier (pkgalias.Func).
func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var obj types.Object
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		obj = info.Uses[fn]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[fn]; ok {
			obj = sel.Obj()
		} else {
			obj = info.Uses[fn.Sel]
		}
	default:
		return nil
	}
	fn, _ := obj.(*types.Func)
	return fn
}

// isCgoPkg reports whether pkg is the cgo pseudo-package boundary: either
// the literal synthetic "C" package go/types builds per C.<ident>
// reference, or a generated runtime/cgo-style support package whose path
// ends in "/cgo". resolve.CgoResolver owns edges across this boundary.
func isCgoPkg(pkg *types.Package) bool {
	return pkg.Name() == "C" || strings.HasSuffix(pkg.Path(), "/cgo")
}

// funcID mints the same go:<pkg>.<recv>.<name> / go:<pkg>.<name> node ID
// GoParser.Parse mints for the same declaration, from a resolved
// *types.Func instead of source text. pkg is fn.Pkg().Name(), the package's
// source name (matching f.Name.Name in GoParser) — not its import path.
func funcID(fn *types.Func) graph.NodeID {
	if fn.Pkg() == nil {
		return ""
	}
	qual := fn.Pkg().Name() + "."
	if recv := recvTypeName(fn); recv != "" {
		qual += recv + "."
	}
	qual += fn.Name()
	return graph.NodeID(ids.Mint("go", qual))
}

// recvTypeName returns the base receiver type name of a method's signature
// ("" for a plain function), unwrapping a pointer receiver. types.Named.Obj
// already excludes any generic instantiation's type arguments (mirrors
// recvName in goparser.go, "drop generics brackets", but working from a
// resolved type instead of an *ast.Expr).
func recvTypeName(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}
