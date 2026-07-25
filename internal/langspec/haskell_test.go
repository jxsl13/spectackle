package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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

// TestHaskellSpecMultiLineSignature pins down R-0005's headline miss: a
// top-level signature with the name alone on its own line and `::` landing
// on the indented continuation line.
func TestHaskellSpecMultiLineSignature(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr, err := p.Parse("app.hs", []byte(`multiply
  :: Int -> Int -> Int
multiply x y = x * y
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["hs:app.multiply"]
	if !ok {
		t.Fatalf("hs:app.multiply missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 1 {
		t.Errorf("multiply = %+v, want KFunc Line=1", n)
	}
	// Exactly one node: the equation line `multiply x y = x * y` must not
	// also mint a second, colliding node.
	count := 0
	for _, nd := range pr.Nodes {
		if nd.ID == "hs:app.multiply" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d nodes named hs:app.multiply, want 1: %+v", count, pr.Nodes)
	}
}

// TestHaskellSpecOperatorDef pins down R-0005's symbolic/operator-named
// top-level definition miss: the primary signature Def's name group cannot
// match a parenthesized operator token at all.
func TestHaskellSpecOperatorDef(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr, err := p.Parse("app.hs", []byte(`(<+>) :: Int -> Int -> Int
(<+>) a b = add a b
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["hs:app.<+>"]
	if !ok {
		t.Fatalf("hs:app.<+> missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 1 {
		t.Errorf("<+> = %+v, want KFunc Line=1", n)
	}
}

// TestHaskellSpecNoCallEdges pins down that haskellSpec deliberately leaves
// CallRe nil (R-0005 scope: Haskell has no braces for cspan.Span to count,
// and layout-sensitive call extraction is out of this line-scanner's
// reach) — zero edges are emitted even when a body plainly calls other
// top-level functions.
func TestHaskellSpecNoCallEdges(t *testing.T) {
	p := SpecParser{S: haskellSpec}
	pr, err := p.Parse("app.hs", []byte(`add :: Int -> Int -> Int
add x y = x + y

main :: IO ()
main = print (add 1 2)
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("got %d edges, want 0 (CallRe deliberately unset for Haskell): %+v", len(pr.Edges), pr.Edges)
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
