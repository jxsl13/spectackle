package index

import (
	"bytes"
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
)

// GoParser is the stdlib-backed Go language parser (never tree-sitter for Go:
// go/parser has strictly better fidelity and keeps the target binary cgo-free).
// It extracts top-level func/method/type/var symbols with 1-based Line/EndLine
// spans (the drift anchor needs EndLine) and best-effort same/cross-package
// call edges resolved by naming convention. The `import "C"` boundary is left
// to resolve.CgoResolver — a C.<ident> selector is deliberately NOT emitted as
// a Go call edge here.
type GoParser struct{}

// Lang identifies the parser's language.
func (GoParser) Lang() graph.Lang { return graph.LangGo }

// Extensions are the file suffixes this parser claims.
func (GoParser) Extensions() []string { return []string{".go"} }

// Parse turns one Go file into symbol nodes and candidate call edges. Call
// edges carry the intended callee ID by convention (go:<pkg>.<fn> for an
// unqualified call, go:<x>.<sel> for a qualified X.Sel call); the indexer
// prunes any whose destination node does not exist after the whole tree is
// indexed. Parsing is deterministic: identical bytes yield identical results
// (SPX-GRA-001).
func (GoParser) Parse(path string, src []byte) (ParseResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return ParseResult{}, err
	}
	pkg := f.Name.Name
	line := func(p token.Pos) int { return fset.Position(p).Line }

	var nodes []graph.Node
	var edges []graph.Edge

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			qual := pkg + "." + d.Name.Name
			kind := graph.KFunc
			if d.Recv != nil && len(d.Recv.List) == 1 {
				if recv := recvName(d.Recv.List[0].Type); recv != "" {
					qual = pkg + "." + recv + "." + d.Name.Name
					kind = graph.KMethod
				}
			}
			id := graph.NodeID(ids.Mint("go", qual))
			nodes = append(nodes, graph.Node{
				ID: id, Kind: kind, Lang: graph.LangGo, File: path,
				Line: line(d.Pos()), EndLine: line(d.End()), Sig: funcSig(fset, d.Type),
			})
			edges = append(edges, callEdges(fset, id, pkg, path, d.Body)...)
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				switch s := sp.(type) {
				case *ast.TypeSpec:
					id := graph.NodeID(ids.Mint("go", pkg+"."+s.Name.Name))
					nodes = append(nodes, graph.Node{
						ID: id, Kind: graph.KType, Lang: graph.LangGo, File: path,
						Line: line(s.Pos()), EndLine: line(s.End()),
					})
				case *ast.ValueSpec:
					for _, nm := range s.Names {
						if nm.Name == "_" {
							continue
						}
						id := graph.NodeID(ids.Mint("go", pkg+"."+nm.Name))
						nodes = append(nodes, graph.Node{
							ID: id, Kind: graph.KVar, Lang: graph.LangGo, File: path,
							Line: line(nm.Pos()), EndLine: line(s.End()),
						})
					}
				}
			}
		}
	}
	return ParseResult{Nodes: nodes, Edges: edges, Hash: sha256.Sum256(src)}, nil
}

// callEdges walks a function body and emits candidate ECall edges. Calls into
// the cgo pseudo-package "C" are skipped (resolve.CgoResolver owns that edge).
func callEdges(fset *token.FileSet, src graph.NodeID, pkg, path string, body *ast.BlockStmt) []graph.Edge {
	if body == nil {
		return nil
	}
	var edges []graph.Edge
	seen := map[graph.NodeID]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var dst graph.NodeID
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			dst = graph.NodeID(ids.Mint("go", pkg+"."+fn.Name))
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && x.Name != "C" {
				dst = graph.NodeID(ids.Mint("go", x.Name+"."+fn.Sel.Name))
			}
		}
		if dst == "" || dst == src || seen[dst] {
			return true
		}
		seen[dst] = true
		edges = append(edges, graph.Edge{
			Src: src, Dst: dst, Kind: graph.ECall, File: path, Line: fset.Position(call.Pos()).Line,
		})
		return true
	})
	return edges
}

// recvName returns the base type name of a method receiver, unwrapping a
// pointer and generic type parameters ("" if it cannot be named).
func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr: // generic receiver T[P]
		return recvName(t.X)
	case *ast.IndexListExpr: // generic receiver T[P, Q]
		return recvName(t.X)
	}
	return ""
}

// funcSig renders a compact signature, e.g. "(n int,a float32,x,y []float32)error".
func funcSig(fset *token.FileSet, ft *ast.FuncType) string {
	var b strings.Builder
	b.WriteString(fieldList(fset, ft.Params, true))
	if ft.Results != nil && len(ft.Results.List) > 0 {
		named := false
		n := 0
		for _, f := range ft.Results.List {
			named = named || len(f.Names) > 0
			n += max(1, len(f.Names))
		}
		if n == 1 && !named {
			b.WriteString(typeString(fset, ft.Results.List[0].Type))
		} else {
			b.WriteString(fieldList(fset, ft.Results, true))
		}
	}
	return b.String()
}

// fieldList renders "(a,b T,c U)" from an ast.FieldList.
func fieldList(fset *token.FileSet, fl *ast.FieldList, parens bool) string {
	var b strings.Builder
	if parens {
		b.WriteByte('(')
	}
	if fl != nil {
		for i, f := range fl.List {
			if i > 0 {
				b.WriteByte(',')
			}
			for j, nm := range f.Names {
				if j > 0 {
					b.WriteByte(',')
				}
				b.WriteString(nm.Name)
			}
			if len(f.Names) > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(typeString(fset, f.Type))
		}
	}
	if parens {
		b.WriteByte(')')
	}
	return b.String()
}

// typeString renders a type expression with all interior whitespace collapsed
// to single spaces (so "[]float32" and "*C.float" stay compact).
func typeString(fset *token.FileSet, e ast.Expr) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "?"
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
