package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// rustSrc exercises every rustSpec Def (fn/struct/enum/trait/const/static)
// plus negative lines that must NOT mint nodes: a struct field decl, an
// if-statement, and a commented-out fn.
var rustSrc = []byte(`pub fn top_level(x: i32) -> i32 {
    x
}

async fn fetch_data() -> i32 {
    0
}

pub(crate) unsafe fn danger() {
}

struct Point {
    x: i32,
    y: i32,
}

pub enum Color {
    Red,
    Green,
}

trait Shape {
    fn area(&self) -> f64;
}

pub const MAX: i32 = 100;

static COUNTER: i32 = 0;

// fn commented_out() {}
if let Some(x) = maybe_thing() {
    println!("{}", x);
}
`)

func TestRustSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: rustSpec}
	if p.Lang() != graph.Lang("rs") {
		t.Errorf("Lang() = %v, want rs", p.Lang())
	}
	if got, want := p.Extensions(), []string{".rs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestRustSpecNodes(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("pkg/app.rs", rustSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"rs:app.top_level":  {graph.KFunc, 1},
		"rs:app.fetch_data": {graph.KFunc, 5},
		"rs:app.danger":     {graph.KFunc, 9},
		"rs:app.Point":      {graph.KType, 12},
		"rs:app.Color":      {graph.KType, 17},
		"rs:app.Shape":      {graph.KType, 22},
		"rs:app.area":       {graph.KFunc, 23},
		"rs:app.MAX":        {graph.KVar, 26},
		"rs:app.COUNTER":    {graph.KVar, 28},
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
		if n.Lang != graph.Lang("rs") {
			t.Errorf("%s Lang = %v, want rs", id, n.Lang)
		}
		if n.File != "pkg/app.rs" {
			t.Errorf("%s File = %q, want pkg/app.rs", id, n.File)
		}
	}
}

// TestRustSpecNegativeLines pins down that a struct field decl and an
// if-statement (neither of which start with fn/struct/enum/trait/const/
// static) never mint nodes, and that a commented-out fn is not matched
// either.
func TestRustSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("neg.rs", []byte(`struct S {
    x: i32,
}
if let Some(x) = maybe_thing() {
    println!("{}", x);
}
// fn commented_out() {}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["rs:neg.x"]; ok {
		t.Error("field decl `x: i32,` must not mint a node")
	}
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only struct S): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["rs:neg.S"]; !ok {
		t.Errorf("struct S missing, got %+v", pr.Nodes)
	}
}

func TestRustSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("rs") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"rs\") — rustSpec not registered via init()")
	}
}

func TestRustSpecDeterministic(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr1, err := p.Parse("pkg/app.rs", rustSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.rs", rustSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
