package index

import (
	"crypto/sha256"
	"path/filepath"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/ids"
	"github.com/jxsl13/spectacle/internal/plan9"
)

// AsmParser is the Plan 9 assembly LanguageParser. It wraps plan9.Scan to
// extract TEXT (KAsmProc) and GLOBL (KVar) symbol definitions; it emits no
// same-language edges (the Go<->asm binding is resolve.Plan9AsmResolver's
// job, edge kind graph.EAsm).
type AsmParser struct{}

// Lang identifies the parser's language.
func (AsmParser) Lang() graph.Lang { return graph.LangAsm }

// Extensions are the file suffixes this parser claims.
func (AsmParser) Extensions() []string { return []string{".s", ".S"} }

// Parse turns one Plan 9 asm file into symbol nodes. Symbols are qualified by
// their containing directory's base name (the Go package convention: a
// mat_amd64.s file living beside package mat mints "mat.mulVec"), matching
// the qualified names GoParser mints for the corresponding Go package so the
// resolver can pair them by ID alone.
func (AsmParser) Parse(path string, src []byte) (ParseResult, error) {
	syms := plan9.Scan(src)
	dirbase := filepath.Base(filepath.Dir(path))

	var nodes []graph.Node
	for _, sym := range syms {
		qual := sym.Name
		if dirbase != "." {
			qual = dirbase + "." + sym.Name
		}
		kind := graph.KVar
		if sym.Kind == "text" {
			kind = graph.KAsmProc
		}
		nodes = append(nodes, graph.Node{
			ID: graph.NodeID(ids.Mint("asm", qual)), Kind: kind, Lang: graph.LangAsm,
			File: path, Line: sym.Line, EndLine: sym.Line, Sig: sym.Frame,
		})
	}
	return ParseResult{Nodes: nodes, Hash: sha256.Sum256(src)}, nil
}
