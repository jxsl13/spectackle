package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// cppSrc exercises every cppSpec Def shape: a class, a struct, an
// out-of-line method definition (Foo::method), a plain function, and
// indented control-flow/return statements that must never mint a node
// (same anchoring argument as cSpec).
var cppSrc = []byte(`#include <cstddef>

class Shape {
public:
    virtual double area() const = 0;
};

struct Point {
    int x, y;
};

double Shape::area() const {
    return 0.0;
}

int add(int a, int b) {
    if (a > b) {
        return a;
    }
    for (int i = 0; i < 10; i++) {
        while (i > 0) {
            switch (i) {
            case 1:
                return b;
            }
        }
    }
    return b;
}
`)

func TestCppSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: cppSpec}
	if p.Lang() != graph.LangCpp {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangCpp)
	}
	if got, want := p.Extensions(), []string{".cc", ".cpp", ".cxx", ".hpp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestCppSpecNodes(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("shapes.cpp", cppSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int // brace-counted body end (LSP-001); KType defs: == Line
	}{
		"cpp:Shape": {graph.KType, 3, 3},
		"cpp:Point": {graph.KType, 8, 8},
		// out-of-line method Shape::area: SpecParser has a single Name
		// submatch group, so only the flat method name (group 2) is
		// captured — the enclosing class qualifier is dropped. This is a
		// documented v0 limitation (see cpp.go's methodRe comment). Its body
		// (lines 12-14) is brace-counted since CallRe is now enabled for
		// cppSpec.
		"cpp:area": {graph.KFunc, 12, 14},
		"cpp:add":  {graph.KFunc, 16, 29},
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
		if n.Lang != graph.LangCpp {
			t.Errorf("%s Lang = %v, want cpp", id, n.Lang)
		}
	}
}

// TestCppSpecNoFalsePositivesOnControlFlow mirrors the cSpec guard: no
// "cpp:if"/"cpp:for"/"cpp:while"/"cpp:switch"/"cpp:return" node from the
// indented control-flow block inside add().
func TestCppSpecNoFalsePositivesOnControlFlow(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("shapes.cpp", cppSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	banned := []graph.NodeID{"cpp:if", "cpp:for", "cpp:while", "cpp:switch", "cpp:return", "cpp:cstddef"}
	byID := cNodesByID(pr)
	for _, id := range banned {
		if n, ok := byID[id]; ok {
			t.Errorf("false positive node %s = %+v, must not be minted from control-flow/include lines", id, n)
		}
	}
}

// TestCppSpecNoEdges still holds with CallRe enabled: cppSrc's bodies
// (Shape::area, add) contain only control-flow keywords and no real calls,
// so it remains a valid "nothing here looks like a call" regression guard.
func TestCppSpecNoEdges(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("x.cpp", cppSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("cppSpec Parse must not emit edges, got %+v", pr.Edges)
	}
}

// cppCallSrc is cppSpec's PART 2 acceptance fixture, mirroring cCallSrc: an
// out-of-line method (Engine::render) calls helper() and printf() (the two
// expected ECall edges) plus every negative case from the task brief —
// `if (`, `sizeof(`, and a recursive self-call (render(), matched by its
// flat method name per cppSpec's Name=2 v0 limitation) — none of which may
// produce an edge.
var cppCallSrc = []byte(`class Engine {
public:
    void render();
};

void Engine::render() {
    if (ready) {
        return;
    }
    int sz = sizeof(int);
    helper();
    printf("launched %d\n", sz);
    render();
}
`)

func TestCppSpecCallEdgesOutOfLineMethod(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("engine.cpp", cppCallSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 2 {
		t.Fatalf("want exactly 2 ECall edges (helper, printf), got %d: %+v", len(pr.Edges), pr.Edges)
	}
	want := map[graph.NodeID]int{"cpp:helper": 11, "cpp:printf": 12}
	got := map[graph.NodeID]int{}
	for _, e := range pr.Edges {
		if e.Src != "cpp:render" {
			t.Errorf("edge Src = %q, want cpp:render: %+v", e.Src, e)
		}
		if e.Kind != graph.ECall {
			t.Errorf("edge Kind = %v, want ECall: %+v", e.Kind, e)
		}
		if e.File != "engine.cpp" {
			t.Errorf("edge File = %q, want engine.cpp: %+v", e.File, e)
		}
		got[e.Dst] = e.Line
	}
	for dst, wantLine := range want {
		gotLine, ok := got[dst]
		if !ok {
			t.Errorf("missing edge to %s, got edges %+v", dst, pr.Edges)
			continue
		}
		if gotLine != wantLine {
			t.Errorf("edge to %s at line %d, want %d", dst, gotLine, wantLine)
		}
	}
	// negatives: `if (`, `sizeof(`, and the recursive self-call to render
	// must never appear as edge destinations.
	for _, banned := range []graph.NodeID{"cpp:if", "cpp:sizeof", "cpp:render"} {
		if line, ok := got[banned]; ok {
			t.Errorf("false positive edge to %s at line %d: if/sizeof/own-name must never become call edges", banned, line)
		}
	}
}

// cppAllmanSrc is T-0053's end-to-end regression, mirroring ddnet's
// universal brace style: the opening '{' sits on the line *after* the def
// line instead of on it. cppSpec's out-of-line method Def (`Foo::Bar(`, see
// cpp.go) has no trailing-anchor requirement, so it already minted a
// cpp:Render node for this shape before T-0053 — what braceSpan's Allman
// extension fixes is that the body (and its call edge to str_copy, a C
// function from ddnet's base/system.c that's heavily called across the
// cpp:/c: FFI boundary) now actually gets scanned instead of collapsing to
// EndLine == Line with zero edges.
var cppAllmanSrc = []byte(`void CClass::Render()
{
	str_copy(m_aBuf, "hello", sizeof(m_aBuf));
}
`)

func TestCppSpecAllmanMethodCallEdges(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("render.cpp", cppAllmanSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)
	n, ok := byID["cpp:Render"]
	if !ok {
		t.Fatalf("node cpp:Render missing, got %+v", pr.Nodes)
	}
	if n.Line != 1 || n.EndLine != 4 {
		t.Errorf("cpp:Render Line/EndLine = %d/%d, want 1/4 (Allman body scanned)", n.Line, n.EndLine)
	}
	if len(pr.Edges) != 1 {
		t.Fatalf("want exactly 1 ECall edge, got %d: %+v", len(pr.Edges), pr.Edges)
	}
	e := pr.Edges[0]
	if e.Src != "cpp:Render" || e.Dst != "cpp:str_copy" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want cpp:Render -ECall-> cpp:str_copy", e)
	}
	if e.File != "render.cpp" || e.Line != 3 {
		t.Errorf("edge File/Line = %q/%d, want render.cpp/3", e.File, e.Line)
	}
}

func TestCppSpecDeterministic(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr1, err := p.Parse("shapes.cpp", cppSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("shapes.cpp", cppSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestCppSpecExtensionsCoverAllVariants(t *testing.T) {
	p := SpecParser{S: cppSpec}
	for _, ext := range []string{".cc", ".cpp", ".cxx", ".hpp"} {
		found := false
		for _, e := range p.Extensions() {
			if e == ext {
				found = true
			}
		}
		if !found {
			t.Errorf("cppSpec.Exts missing %q", ext)
		}
	}
}
