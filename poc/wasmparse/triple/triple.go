// Package triple defines the common (name, kind, line) shape that both the
// cgo/langspec oracle (poc/wasmparse/cparser) and the wazero/tree-sitter
// side (poc/wasmparse/tswasm) reduce their parse output to, so T-0040's
// parity comparison (poc/wasmparse/cmd/poc) never has to know which backend
// produced a given result.
package triple

import "fmt"

// Kind mirrors the subset of graph.NodeKind this PoC cares about, spelled
// out as a string so neither side needs to import internal/graph's numeric
// enum to agree on a shared vocabulary.
type Kind string

const (
	KindFunc Kind = "fn"
	KindType Kind = "type"
)

// Triple is one located symbol definition: a name, a kind, and the 1-based
// source line it was found on.
type Triple struct {
	Name string
	Kind Kind
	Line int
}

func (t Triple) String() string {
	return fmt.Sprintf("%s:%s@%d", t.Kind, t.Name, t.Line)
}

// Key is a comparison key ignoring line, used to pair up cSpec/tree-sitter
// triples across a ±1 line tolerance (T-0040 part 3(a): "tree-sitter names
// the definition line precisely; regex may anchor on the prototype line").
type Key struct {
	Name string
	Kind Kind
}

func (t Triple) Key() Key { return Key{Name: t.Name, Kind: t.Kind} }
