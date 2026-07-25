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

	// EndLine now reflects the true brace-counted body span (R-0005:
	// swiftSpec.CallRe was nil before this fix, which collapsed every
	// node's span to its declaration line regardless of body length).
	// KType nodes are never span-computed (langspec.go only spans
	// KFunc/KMethod), so their EndLine stays equal to Line; `area` (a
	// protocol requirement with no body) and `greet` (single-line body)
	// are also unaffected.
	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"swift:app.topLevel":    {graph.KFunc, 1, 3},
		"swift:app.make":        {graph.KFunc, 5, 7},
		"swift:app.viewDidLoad": {graph.KFunc, 9, 11},
		"swift:app.Foo":         {graph.KType, 13, 13},
		"swift:app.init":        {graph.KFunc, 14, 16},
		"swift:app.Point":       {graph.KType, 19, 19},
		"swift:app.Color":       {graph.KType, 22, 22},
		"swift:app.Shape":       {graph.KType, 26, 26},
		"swift:app.area":        {graph.KFunc, 27, 27},
		"swift:app.Counter":     {graph.KType, 30, 30},
		"swift:app.Bar":         {graph.KType, 33, 33},
		"swift:app.greet":       {graph.KFunc, 34, 34},
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
		if n.Lang != graph.Lang("swift") {
			t.Errorf("%s Lang = %v, want swift", id, n.Lang)
		}
		if n.File != "pkg/app.swift" {
			t.Errorf("%s File = %q, want pkg/app.swift", id, n.File)
		}
	}
}

// TestSwiftSpecOverrideInit pins down R-0005's headline miss: a subclass's
// `override init(...)` didn't match because `override` was missing from the
// init Def's modifier whitelist.
func TestSwiftSpecOverrideInit(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr, err := p.Parse("app.swift", []byte(`class Dog: Animal {
    override init(name: String) {
        super.init(name: name)
    }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["swift:app.init"]
	if !ok {
		t.Fatalf("swift:app.init missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 2 {
		t.Errorf("init = %+v, want KFunc Line=2", n)
	}
}

// TestSwiftSpecFailableInit pins down R-0005's failable/IUO initializer
// miss: the old `(init)\s*\(` shape required the paren immediately after
// the literal `init`, so `init?`/`init!` never matched.
func TestSwiftSpecFailableInit(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr, err := p.Parse("app.swift", []byte(`required init?(coder: NSCoder) {
    super.init()
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["swift:app.init"]; !ok {
		t.Fatalf("swift:app.init missing for failable init, got %+v", pr.Nodes)
	}
}

// TestSwiftSpecSubscriptDeinit pins down R-0005's subscript (medium) and
// deinit (low, but free) misses: neither had a matching Def.
func TestSwiftSpecSubscriptDeinit(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	pr, err := p.Parse("app.swift", []byte(`struct Point {
    subscript(index: Int) -> Double {
        get { 0 }
        set { }
    }
}

class Animal {
    deinit {
        print("bye")
    }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["swift:app.subscript"]; !ok {
		t.Errorf("swift:app.subscript missing, got %+v", pr.Nodes)
	}
	if _, ok := byID["swift:app.deinit"]; !ok {
		t.Errorf("swift:app.deinit missing, got %+v", pr.Nodes)
	}
}

// TestSwiftSpecCallEdges pins down R-0005's most consequential miss: CallRe
// was nil, so zero ECall edges were ever emitted for Swift regardless of
// content — impact BFS (`get`/depth=N) was a permanent no-op.
func TestSwiftSpecCallEdges(t *testing.T) {
	p := SpecParser{S: swiftSpec}
	src := []byte(`struct Point {
    static func hypot(_ a: Double, _ b: Double) -> Double {
        return a + b
    }

    func distance(to other: Point) -> Double {
        return Point.hypot(1, 2)
    }
}

func combine(_ a: Int, _ b: Int) -> Int {
    let sum = addHelper(a, b)
    return sum
}

private func addHelper(_ a: Int, _ b: Int) -> Int {
    return a + b
}
`)
	pr, err := p.Parse("app.swift", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var combineID graph.NodeID
	for _, n := range pr.Nodes {
		if n.ID == "swift:app.combine" {
			combineID = n.ID
		}
	}
	if combineID == "" {
		t.Fatalf("swift:app.combine missing, got %+v", pr.Nodes)
	}
	found := false
	for _, e := range pr.Edges {
		if e.Src == combineID && e.Kind == graph.ECall && e.Dst == "swift:app.addHelper" {
			found = true
		}
	}
	if !found {
		t.Errorf("combine() missing call edge to addHelper; got edges %+v", pr.Edges)
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
