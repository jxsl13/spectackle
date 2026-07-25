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

// TestJuliaSpecNodes' EndLine expectations were updated by T-0119: juliaSpec
// now sets CallRe + EndSpan, so every `function ... end` body here
// keyword-counts to its real span. Short-form one-liners (square) and
// KType defs (struct/abstract type — only KFunc/KMethod are ever
// span-computed) correctly stay Line==EndLine, since they truly have no
// "end"-delimited body to find (or, for KType, are never spanned at all).
// The pre-T-0119 assertion (EndLine always equals Line, even for `function`
// bodies) was pinning down exactly the "span defect" finding this task
// fixes ([high]: "call/launch edges are structurally never emitted ... As a
// direct consequence EndLine is also always == Line"), so updating it here
// is the fix, not a regression.
func TestJuliaSpecNodes(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	pr, err := p.Parse("pkg/app.jl", juliaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"jl:app.run":       {graph.KFunc, 1, 3},
		"jl:app.Base.show": {graph.KFunc, 5, 7},
		"jl:app.push!":     {graph.KFunc, 9, 11},
		"jl:app.square":    {graph.KFunc, 13, 13},
		"jl:app.Point":     {graph.KType, 15, 15},
		"jl:app.Color":     {graph.KType, 20, 20},
		"jl:app.Shape":     {graph.KType, 26, 26},
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
		if n.Lang != graph.LangJl {
			t.Errorf("%s Lang = %v, want jl", id, n.Lang)
		}
		if n.File != "pkg/app.jl" {
			t.Errorf("%s File = %q, want pkg/app.jl", id, n.File)
		}
	}
}

// juliaGapFixtureSrc reuses gap-julia/sample.jl's macro-definition,
// where-clause, and call-edge constructs from R-0005's empirical
// findings — every one of these was a [high] miss before T-0119:
//   - `macro mytime(ex) ... end`: no Def pattern covered macro definitions
//     at all.
//   - `scale(x::T) where T = x * 2`: the short-form Def regex required `=`
//     immediately after the closing paren, so a `where` clause in between
//     broke the match entirely.
//   - `area` calling `helper`/`scale`, and `run_all` calling `normalize!`:
//     zero call edges were ever produced for Julia.
var juliaGapFixtureSrc = []byte(`function area(c::Circle)
    r2 = helper(c.r)
    return scale(r2)
end

helper(x) = x * x

scale(x::T) where T = x * 2

macro mytime(ex)
    return :( @time $(esc(ex)) )
end

function run_all(p::Point)
    @mytime normalize!(p)
    return area(p)
end

function normalize!(p::Point)
    p.x = p.x / 2
    return p
end
`)

func TestJuliaSpecGapFixtureMacroWhereClauseAndEdges(t *testing.T) {
	p := SpecParser{S: juliaSpec}
	pr, err := p.Parse("sample.jl", juliaGapFixtureSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	macro, ok := byID["jl:sample.mytime"]
	if !ok {
		t.Fatalf("jl:sample.mytime (macro def) missing, got %+v", pr.Nodes)
	}
	if macro.Kind != graph.KFunc || macro.Line != 10 || macro.EndLine != 12 {
		t.Errorf("mytime = %+v, want KFunc Line/EndLine 10/12", macro)
	}

	scale, ok := byID["jl:sample.scale"]
	if !ok {
		t.Fatalf("jl:sample.scale (where-clause short-form) missing, got %+v", pr.Nodes)
	}
	if scale.Line != scale.EndLine {
		t.Errorf("scale Line/EndLine = %d/%d, want equal (short-form one-liner, no body)", scale.Line, scale.EndLine)
	}

	// area calls helper and scale; run_all calls normalize! (bang name —
	// CallRe must capture the trailing "!" or this edge resolves to a
	// never-minted "normalize" instead of the real "normalize!" node).
	wantEdges := map[graph.NodeID][]graph.NodeID{
		"jl:sample.area":    {"jl:sample.helper", "jl:sample.scale"},
		"jl:sample.run_all": {"jl:sample.normalize!"},
	}
	for src, dsts := range wantEdges {
		for _, dst := range dsts {
			found := false
			for _, e := range pr.Edges {
				if e.Src == src && e.Dst == dst {
					found = true
				}
			}
			if !found {
				t.Errorf("missing ECall edge %s -> %s, got edges %+v", src, dst, pr.Edges)
			}
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
