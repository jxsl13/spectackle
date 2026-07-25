package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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

	// EndLine now reflects the true brace-counted body span (R-0005:
	// rustSpec.CallRe was nil before this fix, which collapsed every
	// KFunc's span to its declaration line regardless of body length — see
	// TestRustSpecEndLineSpan for a dedicated multi-line-body assertion).
	// KType/KVar nodes are never span-computed (langspec.go only spans
	// KFunc/KMethod), so their EndLine stays equal to Line.
	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"rs:app.top_level":  {graph.KFunc, 1, 3},
		"rs:app.fetch_data": {graph.KFunc, 5, 7},
		"rs:app.danger":     {graph.KFunc, 9, 10},
		"rs:app.Point":      {graph.KType, 12, 12},
		"rs:app.Color":      {graph.KType, 17, 17},
		"rs:app.Shape":      {graph.KType, 22, 22},
		"rs:app.area":       {graph.KFunc, 23, 23}, // trait decl, no body (`;`)
		"rs:app.MAX":        {graph.KVar, 26, 26},
		"rs:app.COUNTER":    {graph.KVar, 28, 28},
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
		if n.Lang != graph.Lang("rs") {
			t.Errorf("%s Lang = %v, want rs", id, n.Lang)
		}
		if n.File != "pkg/app.rs" {
			t.Errorf("%s File = %q, want pkg/app.rs", id, n.File)
		}
	}
}

// TestRustSpecConstFn pins down R-0005's headline miss: `pub const fn` (and
// bare `const fn`) now mint a KFunc node, and — critically — no longer also
// trip the KVar Def into mis-capturing the literal word "fn" as a spurious
// variable node (the pre-fix behavior: KFunc didn't match at all, so KVar's
// `(?:const|static)\s+(\w+)` matched "const fn" and captured "fn").
func TestRustSpecConstFn(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("app.rs", []byte(`pub const fn compute_default() -> i32 {
    42
}

const fn bare() -> i32 {
    0
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["rs:app.compute_default"]; !ok {
		t.Fatalf("rs:app.compute_default missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KFunc || n.Line != 1 || n.EndLine != 3 {
		t.Errorf("compute_default = %+v, want KFunc Line=1 EndLine=3", n)
	}
	if _, ok := byID["rs:app.bare"]; !ok {
		t.Errorf("rs:app.bare missing, got %+v", pr.Nodes)
	}
	if _, ok := byID["rs:app.fn"]; ok {
		t.Error(`spurious "fn" node minted from "const fn" — KVar Def regression`)
	}
}

// TestRustSpecPubCrateStatic pins down R-0005's pub(crate)-scoped
// const/static miss: the old KVar Def only accepted a bare `pub\s+` prefix,
// so any `pub(...)` form failed to match entirely.
func TestRustSpecPubCrateStatic(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("app.rs", []byte(`pub(crate) static REGISTRY: [i32; 4] = [0, 0, 0, 0];
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["rs:app.REGISTRY"]; !ok {
		t.Fatalf("rs:app.REGISTRY missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KVar {
		t.Errorf("REGISTRY Kind = %v, want KVar", n.Kind)
	}
}

// TestRustSpecTypeAlias pins down R-0005's type-alias miss: `type X = ...;`
// had no Def at all.
func TestRustSpecTypeAlias(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("app.rs", []byte(`pub type ShapeList = Vec<Box<dyn Shape>>;
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["rs:app.ShapeList"]; !ok {
		t.Fatalf("rs:app.ShapeList missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("ShapeList Kind = %v, want KType", n.Kind)
	}
}

// TestRustSpecUnion pins down R-0005's union miss (low severity, but free
// given it's one more alternation on the existing struct/enum/trait Def).
func TestRustSpecUnion(t *testing.T) {
	p := SpecParser{S: rustSpec}
	pr, err := p.Parse("app.rs", []byte(`union RawValue {
    i: i32,
    f: f32,
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["rs:app.RawValue"]; !ok {
		t.Fatalf("rs:app.RawValue missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("RawValue Kind = %v, want KType", n.Kind)
	}
}

// TestRustSpecCallEdges pins down R-0005's most consequential miss: CallRe
// was nil, so SpecParser.Parse never brace-spanned a body or scanned it for
// callees — zero ECall edges were ever emitted for Rust, regardless of
// content. Mirrors the fixture's main() calling Point::new/p1.distance/
// Circle::new/c.area/compute_stats.
func TestRustSpecCallEdges(t *testing.T) {
	p := SpecParser{S: rustSpec}
	src := []byte(`struct Point {
    x: f64,
}

impl Point {
    fn new(x: f64) -> Self {
        Point { x }
    }
}

fn compute_stats(data: &[f64]) -> f64 {
    data[0]
}

fn main() {
    let p1 = Point::new(0.0);
    let d = p1.distance(&p1);
    let (m) = compute_stats(&[1.0]);
}
`)
	pr, err := p.Parse("app.rs", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var mainID graph.NodeID
	for _, n := range pr.Nodes {
		if n.ID == "rs:app.main" {
			mainID = n.ID
		}
	}
	if mainID == "" {
		t.Fatalf("rs:app.main missing, got %+v", pr.Nodes)
	}
	callees := map[graph.NodeID]bool{}
	for _, e := range pr.Edges {
		if e.Src == mainID && e.Kind == graph.ECall {
			callees[e.Dst] = true
		}
	}
	for _, want := range []graph.NodeID{"rs:app.new", "rs:app.distance", "rs:app.compute_stats"} {
		if !callees[want] {
			t.Errorf("main() missing call edge to %s; got callees %v", want, callees)
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
