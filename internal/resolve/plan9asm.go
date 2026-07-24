package resolve

import (
	"context"
	"path/filepath"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/ids"
	"github.com/jxsl13/spectackle/internal/plan9"
)

// Plan9AsmResolver links Go declarations to their assembly implementations
// (edge kind EAsm).
//
// Detection contract:
//
//   - A `TEXT ·Name(SB)` (or `TEXT pkg·Name(SB)`) definition in a .s file
//     mints the same qualified name as AsmParser: "<dir-base>.Name", where
//     dir-base is the .s file's containing directory (the Go package
//     convention, e.g. mat_amd64.s in package mat -> "mat.mulVec").
//   - If both go:<dir-base>.Name and asm:<dir-base>.Name already exist as
//     nodes in the graph, the pair yields an EAsm edge
//     go:<dir-base>.Name -> asm:<dir-base>.Name, sited at the TEXT line.
//     Neither side is speculatively created here — the indexer materializes
//     stub nodes for any edge endpoint a parser didn't produce, so a
//     TEXT-only or Go-only side still surfaces as a stub, not a dangling
//     edge.
//   - GLOBL data symbols are not resolved to Go references in M1.5; that
//     needs linkname/relocation analysis and is left for a later milestone.
type Plan9AsmResolver struct{}

// Name identifies the resolver.
func (Plan9AsmResolver) Name() string { return "plan9asm" }

// Langs are the languages whose changes require re-running this resolver.
func (Plan9AsmResolver) Langs() []graph.Lang { return []graph.Lang{graph.LangGo, graph.LangAsm} }

// Resolve scans every .s file for TEXT definitions and pairs each with its
// same-named Go declaration, if both are already present in the graph.
func (Plan9AsmResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	if fs == nil {
		return nil, nil
	}
	var edges []graph.Edge
	for _, path := range fs.ByLang(graph.LangAsm) {
		src, err := fs.Read(path)
		if err != nil {
			continue
		}
		dirbase := filepath.Base(filepath.Dir(path))
		for _, sym := range plan9.Scan(src) {
			if sym.Kind != "text" {
				continue // GLOBL data symbols: not resolved in M1.5
			}
			qual := sym.Name
			if dirbase != "." {
				qual = dirbase + "." + sym.Name
			}
			goID := graph.NodeID(ids.Mint("go", qual))
			asmID := graph.NodeID(ids.Mint("asm", qual))
			if _, ok := g.Node(goID); !ok {
				continue
			}
			if _, ok := g.Node(asmID); !ok {
				continue
			}
			edges = append(edges, graph.Edge{
				Src: goID, Dst: asmID, Kind: graph.EAsm, File: path, Line: sym.Line,
			})
		}
	}
	return edges, nil
}
