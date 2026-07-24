package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// elixirSrc exercises every elixirSpec Def: defmodule (incl. a dotted
// submodule name), def, defp, defmacro, defmacrop, and def names carrying a
// trailing `?`/`!`.
var elixirSrc = []byte(`defmodule Foo do
  def run(x) do
    x
  end

  defp helper(x) do
    x
  end

  defmacro my_macro(x) do
    x
  end

  defmacrop priv_macro(x) do
    x
  end

  def valid?(x) do
    true
  end

  def save!(x) do
    x
  end
end

defmodule Foo.Bar.Baz do
  def nested(x) do
    x
  end
end
`)

func TestElixirSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	if p.Lang() != graph.LangEx {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangEx)
	}
	if got, want := p.Extensions(), []string{".ex", ".exs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestElixirSpecNodes(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr, err := p.Parse("pkg/app.ex", elixirSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"ex:app.Foo":         {graph.KType, 1},
		"ex:app.run":         {graph.KFunc, 2},
		"ex:app.helper":      {graph.KFunc, 6},
		"ex:app.my_macro":    {graph.KFunc, 10},
		"ex:app.priv_macro":  {graph.KFunc, 14},
		"ex:app.valid?":      {graph.KFunc, 18},
		"ex:app.save!":       {graph.KFunc, 22},
		"ex:app.Foo.Bar.Baz": {graph.KType, 27},
		"ex:app.nested":      {graph.KFunc, 28},
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
		if n.Lang != graph.LangEx {
			t.Errorf("%s Lang = %v, want ex", id, n.Lang)
		}
		if n.File != "pkg/app.ex" {
			t.Errorf("%s File = %q, want pkg/app.ex", id, n.File)
		}
	}
}

// TestElixirSpecNegativeLines pins down that a comment, a pipe expression
// referencing a "def"-prefixed identifier, and `defstruct` (which shares a
// prefix with `def` but is a distinct keyword, not `def`/`defp`/`defmacro`/
// `defmacrop`) never mint nodes.
func TestElixirSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr, err := p.Parse("neg.ex", []byte(`# def commented
|> def_something
defstruct [:x, :y]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestElixirSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangEx {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangEx — elixirSpec not registered via init()")
	}
}

func TestElixirSpecDeterministic(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr1, err := p.Parse("pkg/app.ex", elixirSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.ex", elixirSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
