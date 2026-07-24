package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// juliaSrc exercises every juliaSpec Def: long-form `function`, a dotted
// name, a bang name, the column-0 short-form one-liner, `mutable struct`,
// plain `struct`, and `abstract type`.
var juliaSrc = []byte(`function run(x)
    x
end

function Base.show(io, x)
    x
end

function push!(v, x)
    x
end

square(x) = x * x

mutable struct Point
    x
    y
end

struct Color
    r
    g
    b
end

abstract type Shape end
`)

func TestJuliaSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	if p.Lang() != graph.LangJl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangJl)
	}
	if got, want := p.Extensions(), []string{".jl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestJuliaSpecNodes(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	pr, err := p.Parse("pkg/app.jl", juliaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"jl:app.run":       {graph.KFunc, 1},
		"jl:app.Base.show": {graph.KFunc, 5},
		"jl:app.push!":     {graph.KFunc, 9},
		"jl:app.square":    {graph.KFunc, 13},
		"jl:app.Point":     {graph.KType, 15},
		"jl:app.Color":     {graph.KType, 20},
		"jl:app.Shape":     {graph.KType, 26},
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
		if n.Lang != graph.LangJl {
			t.Errorf("%s Lang = %v, want jl", id, n.Lang)
		}
		if n.File != "pkg/app.jl" {
			t.Errorf("%s File = %q, want pkg/app.jl", id, n.File)
		}
	}
}

// TestJuliaSpecNegativeLines pins down that a plain assignment, an
// if-statement whose condition is an `==` comparison of two calls, an
// indented body line, and an indented short-form one-liner never mint
// nodes.
func TestJuliaSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	pr, err := p.Parse("neg.jl", []byte(`x = 5
if foo(x) == bar(y)
    println(x)
end
    indented(x) = x + 1
end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestJuliaSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangJl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangJl — juliaSpec not registered via init()")
	}
}

func TestJuliaSpecDeterministic(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	pr1, err := p.Parse("pkg/app.jl", juliaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.jl", juliaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
