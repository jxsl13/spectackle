package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// ocamlSrc exercises every ocamlSpec Def (let/let rec, type incl. a
// single-type-variable param case (`type 'a forest`) and a parenthesized
// multi-param case (`type ('a, 'b) pair`), module, module type) plus
// negative lines that must NOT mint nodes: an indented nested let, a
// commented-out let, and `let () = ...` (unit pattern, no capturable name).
var ocamlSrc = []byte(`let run x = x + 1

let rec fact n =
  if n = 0 then 1 else n * fact (n - 1)

type tree = Leaf | Node of tree * tree

type 'a forest = Empty | Trees of 'a tree list

type ('a, 'b) pair = { fst : 'a; snd : 'b }

module Foo = struct
  let inner = 1
end

module type SIG = sig
  val run : int -> int
end

(* let commented = 1 *)
let () = print_string "hi"
`)

func TestOcamlSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	if p.Lang() != graph.LangMl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangMl)
	}
	if got, want := p.Extensions(), []string{".ml", ".mli"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestOcamlSpecNodes(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"ml:app.run":    {graph.KFunc, 1},
		"ml:app.fact":   {graph.KFunc, 3},
		"ml:app.tree":   {graph.KType, 6},
		"ml:app.forest": {graph.KType, 8},
		"ml:app.pair":   {graph.KType, 10},
		"ml:app.Foo":    {graph.KType, 12},
		"ml:app.SIG":    {graph.KType, 16},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != w.Line {
			t.Errorf("%s Line/EndLine = %d/%d, want %d", id, n.Line, n.EndLine, w.Line)
		}
		if n.Lang != graph.LangMl {
			t.Errorf("%s Lang = %v, want ml", id, n.Lang)
		}
		if n.File != "pkg/app.ml" {
			t.Errorf("%s File = %q, want pkg/app.ml", id, n.File)
		}
	}
}

// TestOcamlSpecNegativeLines pins down that a nested indented let, a
// commented-out let, and the unit-pattern `let () = ...` never mint nodes.
func TestOcamlSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr, err := p.Parse("neg.ml", []byte(`module Foo = struct
  let inner = 1
end
(* let commented = 1 *)
let () = print_string "hi"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["ml:neg.inner"]; ok {
		t.Error("indented nested `let inner = 1` must not mint a node")
	}
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only module Foo): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["ml:neg.Foo"]; !ok {
		t.Errorf("module Foo missing, got %+v", pr.Nodes)
	}
}

func TestOcamlSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangMl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangMl — ocamlSpec not registered via init()")
	}
}

func TestOcamlSpecDeterministic(t *testing.T) {
	p := SpecParser{S: ocamlSpec}
	pr1, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.ml", ocamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
