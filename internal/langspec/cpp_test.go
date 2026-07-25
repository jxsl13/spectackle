package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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
// cpp:Render node for this shape before T-0053 — what the Allman extension
// (originally langspec.braceSpan, extracted verbatim into internal/cspan by
// T-0054) fixes is that the body (and its call edge to str_copy, a C
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

// cppT0122Src is R-0005's gap-cpp fixture (scratchpad/gap-cpp/sample.cpp),
// copied verbatim as the regression input for T-0122's cpp.go hardening:
// inline class-body methods (a one-liner getter and a same-line-brace
// virtual override), a destructor, a nested type, `enum class`, an
// out-of-line template method (`Stack<T>::push`), an operator overload, and
// a multi-line free-function signature.
var cppT0122Src = []byte(`struct Point {
    int x;
    int y;
};

class Shape {
public:
    virtual double describe() const {
        return 0.0;
    }
    virtual ~Shape() {}
};

class Circle : public Shape {
public:
    Circle(double r);
    double area() const;
    double radius() const { return radius_; }

private:
    double radius_;
};

double Circle::area() const {
    return 3.14159 * radius_ * radius_;
}

std::ostream& operator<<(std::ostream& os, const Circle& c) {
    os << c.radius();
    return os;
}

template <typename T>
class Stack {
public:
    void push(const T& value);

private:
    std::vector<T> data_;
};

template <typename T>
void Stack<T>::push(const T& value) {
    data_.push_back(value);
}

template <typename T>
T add(T a, T b) {
    return a + b;
}

enum class Color { Red, Green, Blue };

static int helper(int n,
                   int m) {
    return add(n, m);
}

class Outer {
public:
    struct Inner {
        int value;
    };
};
`)

// TestCppSpecT0122InlineClassBodyMethods covers R-0005 cpp.md's [high]
// "Inline class-body method definitions" finding: an indented one-liner
// getter (`radius`) and an indented same-line-brace virtual override
// (`describe`, body on following lines) were both previously invisible
// because the free-function Def is anchored at column zero.
func TestCppSpecT0122InlineClassBodyMethods(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("x.cpp", cppT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	if n, ok := byID["cpp:describe"]; !ok {
		t.Fatalf("node cpp:describe missing, got %+v", pr.Nodes)
	} else {
		if n.Kind != graph.KMethod {
			t.Errorf("cpp:describe Kind = %v, want KMethod", n.Kind)
		}
		if n.Line != 8 || n.EndLine != 10 {
			t.Errorf("cpp:describe Line/EndLine = %d/%d, want 8/10", n.Line, n.EndLine)
		}
	}
	if n, ok := byID["cpp:radius"]; !ok {
		t.Fatalf("node cpp:radius missing, got %+v", pr.Nodes)
	} else {
		if n.Kind != graph.KMethod {
			t.Errorf("cpp:radius Kind = %v, want KMethod", n.Kind)
		}
		if n.Line != 18 || n.EndLine != 18 {
			t.Errorf("cpp:radius Line/EndLine = %d/%d, want 18/18", n.Line, n.EndLine)
		}
	}
}

// TestCppSpecT0122Destructor covers R-0005 cpp.md's [high] "Destructor
// definitions" finding: `~Shape` can never be captured by the plain
// free-function Def because `~` is not a word character.
func TestCppSpecT0122Destructor(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("x.cpp", cppT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)
	n, ok := byID["cpp:~Shape"]
	if !ok {
		t.Fatalf("node cpp:~Shape missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KMethod {
		t.Errorf("cpp:~Shape Kind = %v, want KMethod", n.Kind)
	}
	if n.Line != 11 {
		t.Errorf("cpp:~Shape Line = %d, want 11", n.Line)
	}
	// The destructor's own name must never collide with its class's KType
	// node: "Shape" (the class) and "~Shape" (the destructor) are distinct
	// NodeIDs.
	if _, ok := byID["cpp:Shape"]; !ok {
		t.Error("node cpp:Shape (the class) missing — destructor fix must not clobber the class's own KType node")
	}
}

// TestCppSpecT0122NestedTypeAndEnumClass covers R-0005 cpp.md's [high]
// "Nested type declarations" and [high] "enum / enum class type
// declarations" findings.
func TestCppSpecT0122NestedTypeAndEnumClass(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("x.cpp", cppT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	if n, ok := byID["cpp:Inner"]; !ok {
		t.Fatalf("node cpp:Inner missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("cpp:Inner Kind = %v, want KType", n.Kind)
	}
	if n, ok := byID["cpp:Color"]; !ok {
		t.Fatalf("node cpp:Color missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("cpp:Color Kind = %v, want KType", n.Kind)
	}
}

// TestCppSpecT0122TemplateMethodOperatorAndMultiLineSig covers R-0005
// cpp.md's [high] "Out-of-line template method definitions"
// (`Stack<T>::push`), [medium] "Operator overload definitions"
// (`operator<<`), and [medium] "Multi-line function signatures" (`static
// int helper(int n,\n int m) {`) findings, plus the cascading call edge
// (helper -> add) that a missed multi-line signature used to erase.
func TestCppSpecT0122TemplateMethodOperatorAndMultiLineSig(t *testing.T) {
	p := SpecParser{S: cppSpec}
	pr, err := p.Parse("x.cpp", cppT0122Src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	if n, ok := byID["cpp:push"]; !ok {
		t.Fatalf("node cpp:push missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KFunc {
		t.Errorf("cpp:push Kind = %v, want KFunc", n.Kind)
	}
	if n, ok := byID["cpp:operator<<"]; !ok {
		t.Fatalf("node cpp:operator<< missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KFunc {
		t.Errorf("cpp:operator<< Kind = %v, want KFunc", n.Kind)
	}
	n, ok := byID["cpp:helper"]
	if !ok {
		t.Fatalf("node cpp:helper missing, got %+v", pr.Nodes)
	}
	if n.Line != 54 || n.EndLine != 57 {
		t.Errorf("cpp:helper Line/EndLine = %d/%d, want 54/57", n.Line, n.EndLine)
	}
	foundEdge := false
	for _, e := range pr.Edges {
		if e.Src == "cpp:helper" && e.Dst == "cpp:add" && e.Kind == graph.ECall {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("missing edge cpp:helper -> cpp:add, got edges %+v", pr.Edges)
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
