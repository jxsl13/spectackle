package resolve

import (
	"context"

	"github.com/jxsl13/spectacle/internal/graph"
)

// Plan9AsmResolver links Go declarations to their assembly implementations
// (edge kind EAsm).
//
// Detection contract (implemented in M2):
//
//   - A bodyless function declaration `func Name(...)` in a .go file (often
//     annotated //go:noescape) is an asm stub candidate.
//   - A `TEXT ·Name(SB)` (or `TEXT pkg·Name(SB)`) definition in a .s file of
//     the same package directory (any GOARCH suffix) is its implementation;
//     the pair yields an EAsm edge go:pkg.Name -> asm:pkg.Name.
//   - GLOBL data symbols referenced from Go via linkname get EUse edges.
type Plan9AsmResolver struct{}

func (Plan9AsmResolver) Name() string { return "plan9asm" }

func (Plan9AsmResolver) Langs() []graph.Lang { return []graph.Lang{graph.LangGo, graph.LangAsm} }

func (Plan9AsmResolver) Resolve(ctx context.Context, g graph.Graph, fs FileSet) ([]graph.Edge, error) {
	return nil, nil // M2
}
