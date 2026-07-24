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
		Kind graph.NodeKind
		Line int
	}{
		"cpp:Shape": {graph.KType, 3},
		"cpp:Point": {graph.KType, 8},
		// out-of-line method Shape::area: SpecParser has a single Name
		// submatch group, so only the flat method name (group 2) is
		// captured — the enclosing class qualifier is dropped. This is a
		// documented v0 limitation (see cpp.go's methodRe comment).
		"cpp:area": {graph.KFunc, 12},
		"cpp:add":  {graph.KFunc, 16},
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
