package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// haskellSrc exercises every haskellSpec Def (top-level type signature,
// data/newtype/type, class with and without a context) plus negative lines
// that must NOT mint nodes: an indented signature inside a class body, a
// commented-out signature, and an `instance` declaration.
var haskellSrc = []byte(`run :: Int -> Int
run x = x + 1

helper' :: Int -> Int
helper' x = helper' x

data Tree = Leaf | Node Tree Tree

newtype Wrapper = Wrapper Int

type Alias = Int

class (Eq a) => Container a where
  empty :: a

class Show a where
  render :: a -> String

instance Show Foo where
  render _ = "foo"

  helper :: Int -> Int
-- foo :: Int -> Int
`)

func TestHaskellSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	if p.Lang() != graph.LangHs {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangHs)
	}
	if got, want := p.Extensions(), []string{".hs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestHaskellSpecNodes(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr, err := p.Parse("pkg/app.hs", haskellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"hs:app.run":       {graph.KFunc, 1},
		"hs:app.helper'":   {graph.KFunc, 4},
		"hs:app.Tree":      {graph.KType, 7},
		"hs:app.Wrapper":   {graph.KType, 9},
		"hs:app.Alias":     {graph.KType, 11},
		"hs:app.Container": {graph.KType, 13},
		"hs:app.Show":      {graph.KType, 16},
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
		if n.Lang != graph.LangHs {
			t.Errorf("%s Lang = %v, want hs", id, n.Lang)
		}
		if n.File != "pkg/app.hs" {
			t.Errorf("%s File = %q, want pkg/app.hs", id, n.File)
		}
	}
}

// TestHaskellSpecNegativeLines pins down that an indented type signature
// inside a where/class body, a commented-out signature, and an `instance`
// declaration never mint nodes.
func TestHaskellSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr, err := p.Parse("neg.hs", []byte(`instance Show Foo where
  render _ = "foo"

  helper :: Int -> Int
-- foo :: Int -> Int
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestHaskellSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangHs {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangHs — haskellSpec not registered via init()")
	}
}

func TestHaskellSpecDeterministic(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr1, err := p.Parse("pkg/app.hs", haskellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.hs", haskellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
