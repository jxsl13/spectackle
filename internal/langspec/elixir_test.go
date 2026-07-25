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

// TestElixirSpecNodes' EndLine expectations were updated by T-0119:
// elixirSpec now sets CallRe + EndSpan, so every `do ... end` body here
// keyword-counts to its real span (KType defs — defmodule — stay
// Line==EndLine, since only KFunc/KMethod are ever span-computed). The
// pre-T-0119 assertion (EndLine always equals Line, even for KFunc) was
// pinning down exactly the "span defect" finding this task fixes ([high]:
// "Every node's span collapses to a single line ... hits every multi-line
// function"), so updating it here is the fix, not a regression.
func TestElixirSpecNodes(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr, err := p.Parse("pkg/app.ex", elixirSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"ex:app.Foo":         {graph.KType, 1, 1},
		"ex:app.run":         {graph.KFunc, 2, 4},
		"ex:app.helper":      {graph.KFunc, 6, 8},
		"ex:app.my_macro":    {graph.KFunc, 10, 12},
		"ex:app.priv_macro":  {graph.KFunc, 14, 16},
		"ex:app.valid?":      {graph.KFunc, 18, 20},
		"ex:app.save!":       {graph.KFunc, 22, 24},
		"ex:app.Foo.Bar.Baz": {graph.KType, 27, 27},
		"ex:app.nested":      {graph.KFunc, 28, 30},
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
		if n.Line != w.Line || n.EndLine != w.EndLine {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, w.EndLine)
		}
		if n.Lang != graph.LangEx {
			t.Errorf("%s Lang = %v, want ex", id, n.Lang)
		}
		if n.File != "pkg/app.ex" {
			t.Errorf("%s File = %q, want pkg/app.ex", id, n.File)
		}
	}
}

// elixirGapFixtureSrc reuses gap-elixir/calculator.ex's defguard,
// defdelegate, defprotocol/defimpl, and call-edge constructs from R-0005's
// empirical findings — every one of these was a [high] miss before T-0119:
//   - `defguard is_positive(n) when ...`: no Def pattern covered defguard
//     at all.
//   - `defdelegate delegated_double(x), to: Utils, as: :double_value`: no
//     Def pattern covered defdelegate at all.
//   - `defprotocol ... do ... end` / `defimpl ..., for: Tuple do ... end`:
//     no KType node was ever minted for either.
//   - `run_all` calling `Utils.double_value(sum(1, 2))`: zero call edges
//     were ever produced for Elixir.
var elixirGapFixtureSrc = []byte(`defmodule Calc do
  defmodule Utils do
    def double_value(x) do
      x * 2
    end
  end

  defguard is_positive(n) when is_integer(n) and n > 0

  defdelegate delegated_double(x), to: Utils, as: :double_value

  def sum(a, b), do: a + b

  def run_all do
    Utils.double_value(sum(1, 2))
  end
end

defprotocol Calc.Shape do
  def perimeter(shape)
end

defimpl Calc.Shape, for: Tuple do
  def perimeter({:circle, r}) do
    2 * :math.pi() * r
  end
end
`)

func TestElixirSpecGapFixtureGuardDelegateProtocolAndEdges(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr, err := p.Parse("calc.ex", elixirGapFixtureSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	guard, ok := byID["ex:calc.is_positive"]
	if !ok {
		t.Fatalf("ex:calc.is_positive (defguard) missing, got %+v", pr.Nodes)
	}
	if guard.Kind != graph.KFunc || guard.Line != guard.EndLine {
		t.Errorf("is_positive = %+v, want KFunc with Line==EndLine (no body, single-line defguard)", guard)
	}

	delegate, ok := byID["ex:calc.delegated_double"]
	if !ok {
		t.Fatalf("ex:calc.delegated_double (defdelegate) missing, got %+v", pr.Nodes)
	}
	if delegate.Kind != graph.KFunc || delegate.Line != delegate.EndLine {
		t.Errorf("delegated_double = %+v, want KFunc with Line==EndLine (no body, single-line defdelegate)", delegate)
	}

	if n, ok := byID["ex:calc.Calc.Shape"]; !ok || n.Kind != graph.KType {
		t.Errorf("ex:calc.Calc.Shape (defprotocol) missing or wrong kind, got %+v ok=%v", n, ok)
	}

	// def sum(a, b), do: a + b — the single-line "do:" colon form has no
	// matching "end" anywhere; Open must not mistake the "do" inside "do:"
	// for a real block opener (word-boundary triggers between "o" and ":"
	// just as it does between letters), so this must stay a true one-liner.
	sum, ok := byID["ex:calc.sum"]
	if !ok {
		t.Fatalf("ex:calc.sum missing, got %+v", pr.Nodes)
	}
	if sum.Line != sum.EndLine {
		t.Errorf("sum Line/EndLine = %d/%d, want equal (do: colon form has no body to span, and must not false-open on \"do:\")", sum.Line, sum.EndLine)
	}

	// run_all calls Utils.double_value(...) and sum(...); CallRe strips the
	// "Utils." qualifier (same behavior as every other CallRe'd langspec
	// Spec), so the edge lands on the bare "double_value" name.
	wantEdges := map[graph.NodeID]bool{
		"ex:calc.double_value": false,
		"ex:calc.sum":          false,
	}
	for _, e := range pr.Edges {
		if e.Src != "ex:calc.run_all" {
			continue
		}
		if _, ok := wantEdges[e.Dst]; ok {
			wantEdges[e.Dst] = true
		}
	}
	for dst, got := range wantEdges {
		if !got {
			t.Errorf("missing ECall edge ex:calc.run_all -> %s, got edges %+v", dst, pr.Edges)
		}
	}
}

// TestElixirSpecGuardClauseBeforeDoIsKnownGap documents the one residual
// limitation T-0119 could not fix from this file alone: a multi-clause def
// whose guard wraps onto its own line before "do" (this shape, taken
// verbatim from gap-elixir/calculator.ex's `classify/1`) keeps
// EndLine == Line, because cspan.KeywordSpan (frozen by T-0118) requires
// the opening keyword on the SAME line as the Def match, and here "do" is
// on line 2 while the Def match is on line 1. This is not a regression —
// it's the same Line-only result this construct always had — just not
// improved for this one shape. The Name/Line/Kind themselves are still
// captured correctly.
func TestElixirSpecGuardClauseBeforeDoIsKnownGap(t *testing.T) {
	p := SpecParser{S: elixirSpec}
	pr, err := p.Parse("calc.ex", []byte(`defmodule Calc do
  def classify(n)
      when is_integer(n) and n > 0 do
    :positive
  end
end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["ex:calc.classify"]
	if !ok {
		t.Fatalf("ex:calc.classify missing, got %+v", pr.Nodes)
	}
	if n.Line != 2 {
		t.Errorf("classify Line = %d, want 2", n.Line)
	}
	if n.EndLine != n.Line {
		t.Errorf("classify EndLine = %d, want %d (documented gap: guard-before-do can't be keyword-spanned)", n.EndLine, n.Line)
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
