package resolve

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
)

// CgoResolver links the Go <-> C boundary (edge kind ECgo).
//
// Detection contract:
//
//   - A Go file with `import "C"` is a cgo file. Every call C.<ident>(…) whose
//     ident is not a C primitive-type conversion yields an ECgo edge from the
//     enclosing Go function node to node "c:<ident>". The C symbol is left
//     unresolved here (the C parser lands in M2); the indexer materializes
//     "c:<ident>" as a stub node so the impact radius stays complete.
//   - A `//export Name` directive immediately above a Go function yields the
//     reverse ECgo edge from "c:Name" to that Go function node (C calling into
//     Go).
//
// #include preamble edges (Go file -> header file) arrive with file-level nodes
// in a later milestone; they are disconnected from the symbol radius without
// them, so M1 omits them to keep `get depth` results clean.
type CgoResolver struct{}

// Name identifies the resolver.
func (CgoResolver) Name() string { return "cgo" }

// Langs are the languages whose changes require re-running this resolver.
func (CgoResolver) Langs() []graph.Lang { return []graph.Lang{graph.LangGo, graph.LangC} }

// cPrimitives are cgo built-in names whose C.<name>(x) form is a type
// conversion or a cgo helper, not a call into a repo C symbol — no edge.
// Real libc symbols (C.malloc, C.free, …) DO get edges: they become stub
// nodes, keeping the impact radius complete.
var cPrimitives = map[string]bool{
	"int": true, "uint": true, "char": true, "uchar": true, "schar": true,
	"short": true, "ushort": true, "long": true, "ulong": true,
	"longlong": true, "ulonglong": true, "float": true, "double": true,
	"complexfloat": true, "complexdouble": true, "void": true, "_Bool": true,
	"size_t": true, "ssize_t": true, "ptrdiff_t": true, "intptr_t": true,
	"uintptr_t": true, "int8_t": true, "int16_t": true, "int32_t": true,
	"int64_t": true, "uint8_t": true, "uint16_t": true, "uint32_t": true,
	"uint64_t": true,
	// cgo helper functions (string/byte marshalling), not C symbols:
	"GoString": true, "CString": true, "CBytes": true,
	"GoBytes": true, "GoStringN": true,
}

// Resolve parses every Go file and emits the cgo boundary edges.
func (CgoResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	for _, path := range fs.ByLang(graph.LangGo) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		if !hasCImport(src) {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		pkg := f.Name.Name
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fid := goFuncID(pkg, fn)
			// forward edges: C.<ident>(…) calls inside the body. Bodyless
			// declarations (asm stubs, linkname targets) have no calls.
			if fn.Body == nil {
				continue
			}
			seen := map[string]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok || x.Name != "C" {
					return true
				}
				name := sel.Sel.Name
				if cPrimitives[name] || seen[name] {
					return true
				}
				seen[name] = true
				edges = append(edges, graph.Edge{
					Src: fid, Dst: graph.NodeID("c:" + name), Kind: graph.ECgo,
					File: path, Line: fset.Position(call.Pos()).Line,
				})
				return true
			})
			// reverse edge: //export Name above a Go function (C calls into Go)
			if name := exportName(fn); name != "" {
				edges = append(edges, graph.Edge{
					Src: graph.NodeID("c:" + name), Dst: fid, Kind: graph.ECgo,
					File: path, Line: fset.Position(fn.Pos()).Line,
				})
			}
		}
	}
	return edges, nil
}

// hasCImport reports whether the source imports the cgo pseudo-package "C".
func hasCImport(src []byte) bool {
	return strings.Contains(string(src), "\"C\"")
}

// goFuncID mints the node ID a GoParser would give this function/method, so
// cgo edges attach to the exact same nodes.
func goFuncID(pkg string, fn *ast.FuncDecl) graph.NodeID {
	qual := pkg + "." + fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		if r := recvName(fn.Recv.List[0].Type); r != "" {
			qual = pkg + "." + r + "." + fn.Name.Name
		}
	}
	return graph.NodeID(ids.Mint("go", qual))
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	}
	return ""
}

// exportName returns the symbol from a `//export Name` directive in the
// function's doc comment, or "".
func exportName(fn *ast.FuncDecl) string {
	if fn.Doc == nil {
		return ""
	}
	for _, c := range fn.Doc.List {
		if rest, ok := strings.CutPrefix(c.Text, "//export "); ok {
			if name := strings.TrimSpace(rest); name != "" {
				return name
			}
		}
	}
	return ""
}
