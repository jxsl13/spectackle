package resolve

import (
	"context"

	"github.com/jxsl13/spectacle/internal/graph"
)

// CgoResolver links the Go <-> C boundary (edge kind ECgo).
//
// Detection contract (implemented in M2):
//
//   - A Go file with `import "C"` marks the comment block immediately above
//     the import as the cgo preamble. #include directives in the preamble
//     yield EIncl edges from the Go file node to the header file nodes.
//   - Every selector reference C.<ident> in the Go file yields an ECgo edge
//     from the enclosing Go function node to node "c:<ident>". The C symbol
//     is matched against declarations found in the included headers
//     (transitively, within the repo); unresolved idents still get the edge —
//     the "c:" node is created as a stub so the impact radius stays complete.
//   - A `//export Name` directive above a Go function yields the reverse
//     ECgo edge from "c:Name" to the Go function node (C calling into Go).
type CgoResolver struct{}

func (CgoResolver) Name() string { return "cgo" }

func (CgoResolver) Langs() []graph.Lang { return []graph.Lang{graph.LangGo, graph.LangC} }

func (CgoResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	return nil, nil // M2
}
