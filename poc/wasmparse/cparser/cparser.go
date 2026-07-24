// Package cparser wraps spectacle's own internal/langspec cSpec (the
// regex-driven C parser already shipping in production, see
// internal/langspec/c.go) as this PoC's parity oracle: whatever it finds on
// the corpus is "ground truth" for T-0040's exit criterion (a).
package cparser

import (
	"fmt"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/index"
	"github.com/jxsl13/spectacle/internal/langspec"
	"github.com/jxsl13/spectacle/poc/wasmparse/triple"
)

// Parser is the oracle: internal/langspec's cSpec, obtained the same way
// production wires it up (langspec.All(), filtered to graph.LangC) so this
// PoC never depends on cSpec's unexported identifier directly.
type Parser struct {
	p index.LanguageParser
}

// New locates the registered C langspec.Spec via langspec.All(). Returns an
// error if none is registered (would mean internal/langspec/c.go's init()
// registration was removed or renamed out from under this PoC).
func New() (*Parser, error) {
	for _, p := range langspec.All() {
		if p.Lang() == graph.LangC {
			return &Parser{p: p}, nil
		}
	}
	return nil, fmt.Errorf("cparser: no langspec.Spec registered for graph.LangC")
}

// Parse runs the oracle parser over one file's source and reduces its nodes
// to the common triple.Triple shape. NodeID is "<lang>:<qualified-name>";
// since cSpec uses langspec.QualFlat, qualified-name == the bare matched
// symbol name (internal/langspec/c.go), so stripping the "c:" prefix
// recovers it exactly. Only KFunc/KType nodes are mapped — cSpec never
// emits anything else (see internal/langspec/c.go's Defs).
func (p *Parser) Parse(path string, src []byte) ([]triple.Triple, error) {
	res, err := p.p.Parse(path, src)
	if err != nil {
		return nil, err
	}
	out := make([]triple.Triple, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		name := string(n.ID)
		const prefix = "c:"
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			name = name[len(prefix):]
		}
		var k triple.Kind
		switch n.Kind {
		case graph.KFunc:
			k = triple.KindFunc
		case graph.KType:
			k = triple.KindType
		default:
			continue
		}
		out = append(out, triple.Triple{Name: name, Kind: k, Line: n.Line})
	}
	return out, nil
}
