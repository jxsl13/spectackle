package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// swiftSrc exercises every swiftSpec Def: a plain top-level func, a
// modifier-prefixed (public static) func, an override func, a class with
// an init, a struct, an enum, a protocol (plus a func in its body), an
// actor, and an extension (plus a func in its body).
var swiftSrc = []byte(`func topLevel(x: Int) -> Int {
    return x
}

public static func make() -> Foo {
    return Foo()
}

override func viewDidLoad() {
    super.viewDidLoad()
}

class Foo {
    init(x: Int) {
        self.x = x
    }
}

struct Point {
}

enum Color {
    case red, green
}

protocol Shape {
    func area() -> Double
}

actor Counter {
}

extension Bar {
    func greet() {}
}
`)

func TestSwiftSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	if p.Lang() != graph.Lang("swift") {
		t.Errorf("Lang() = %v, want swift", p.Lang())
	}
	if got, want := p.Extensions(), []string{".swift"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestSwiftSpecNodes(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr, err := p.Parse("pkg/app.swift", swiftSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"swift:app.topLevel":    {graph.KFunc, 1},
		"swift:app.make":        {graph.KFunc, 5},
		"swift:app.viewDidLoad": {graph.KFunc, 9},
		"swift:app.Foo":         {graph.KType, 13},
		"swift:app.init":        {graph.KFunc, 14},
		"swift:app.Point":       {graph.KType, 19},
		"swift:app.Color":       {graph.KType, 22},
		"swift:app.Shape":       {graph.KType, 26},
		"swift:app.area":        {graph.KFunc, 27},
		"swift:app.Counter":     {graph.KType, 30},
		"swift:app.Bar":         {graph.KType, 33},
		"swift:app.greet":       {graph.KFunc, 34},
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
		if n.Lang != graph.Lang("swift") {
			t.Errorf("%s Lang = %v, want swift", id, n.Lang)
		}
		if n.File != "pkg/app.swift" {
			t.Errorf("%s File = %q, want pkg/app.swift", id, n.File)
		}
	}
}

// TestSwiftSpecNegativeLines pins down that a closure literal
// (`{ (x: Int) in`), a call site (`foo()`), and a property decl
// (`var count: Int = 0`) never mint nodes.
func TestSwiftSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr, err := p.Parse("neg.swift", []byte(`let closure = { (x: Int) in
    return x * 2
}
foo()
var count: Int = 0
struct Neg {
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["swift:neg.foo"]; ok {
		t.Error("call site `foo()` must not mint a node")
	}
	if _, ok := byID["swift:neg.count"]; ok {
		t.Error("property decl `var count: Int = 0` must not mint a node")
	}
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only struct Neg): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["swift:neg.Neg"]; !ok {
		t.Errorf("struct Neg missing, got %+v", pr.Nodes)
	}
}

func TestSwiftSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("swift") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"swift\") — swiftSpec not registered via init()")
	}
}

func TestSwiftSpecDeterministic(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr1, err := p.Parse("pkg/app.swift", swiftSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.swift", swiftSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
