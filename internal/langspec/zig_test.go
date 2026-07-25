package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// zigSrc exercises every zigSpec Def: plain `fn`, `pub fn`, `export fn`,
// `extern "c" fn`, `pub inline fn`, and the type forms `const = struct`,
// `pub const = enum`, `const = opaque`, `const = union`, and
// `const = packed struct`.
var zigSrc = []byte(`fn run() void {
}

pub fn add(a: i32, b: i32) i32 {
    return a + b;
}

export fn exported() void {
}

extern "c" fn cImported() void;

pub inline fn fast() void {
}

const Point = struct {
    x: i32,
    y: i32,
};

pub const Color = enum {
    Red,
    Green,
};

const Handle = opaque {};

const Shape = union {
    a: i32,
    b: f64,
};

const Packed = packed struct {
    flag: bool,
};
`)

func TestZigSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: zigSpec}
	if p.Lang() != graph.LangZig {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangZig)
	}
	if got, want := p.Extensions(), []string{".zig"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestZigSpecNodes(t *testing.T) {
	p := SpecParser{S: zigSpec}
	pr, err := p.Parse("pkg/app.zig", zigSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	// EndLine now reflects the true brace-counted body span (R-0005:
	// zigSpec.CallRe was nil before this fix, which collapsed every KFunc
	// node's span to its declaration line regardless of body length).
	// KType nodes are never span-computed (langspec.go only spans
	// KFunc/KMethod), so their EndLine stays equal to Line; `cImported`
	// (an extern decl with no body, ends in `;`) is also unaffected.
	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"zig:app.run":       {graph.KFunc, 1, 2},
		"zig:app.add":       {graph.KFunc, 4, 6},
		"zig:app.exported":  {graph.KFunc, 8, 9},
		"zig:app.cImported": {graph.KFunc, 11, 11},
		"zig:app.fast":      {graph.KFunc, 13, 14},
		"zig:app.Point":     {graph.KType, 16, 16},
		"zig:app.Color":     {graph.KType, 21, 21},
		"zig:app.Handle":    {graph.KType, 26, 26},
		"zig:app.Shape":     {graph.KType, 28, 28},
		"zig:app.Packed":    {graph.KType, 33, 33},
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
		if n.Lang != graph.LangZig {
			t.Errorf("%s Lang = %v, want zig", id, n.Lang)
		}
		if n.File != "pkg/app.zig" {
			t.Errorf("%s File = %q, want pkg/app.zig", id, n.File)
		}
	}
}

// TestZigSpecErrorSet pins down R-0005's headline miss: `const X =
// error{...}` — Zig's idiomatic error-set type — wasn't recognized because
// the type Def only accepted struct/enum/union/opaque after `=`.
func TestZigSpecErrorSet(t *testing.T) {
	p := SpecParser{S: zigSpec}
	pr, err := p.Parse("app.zig", []byte(`const MyError = error{
    OutOfMemory,
    InvalidInput,
};
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["zig:app.MyError"]
	if !ok {
		t.Fatalf("zig:app.MyError missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KType || n.Line != 1 {
		t.Errorf("MyError = %+v, want KType Line=1", n)
	}
}

// TestZigSpecTestBlock pins down R-0005's low-severity (but free) miss:
// `test "..." { ... }` blocks, present in virtually every idiomatic Zig
// file, weren't modeled as any node kind.
func TestZigSpecTestBlock(t *testing.T) {
	p := SpecParser{S: zigSpec}
	pr, err := p.Parse("app.zig", []byte(`test "add works" {
    try std.testing.expect(add(2, 3) == 5);
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["zig:app.test"]; !ok {
		t.Errorf("zig:app.test missing, got %+v", pr.Nodes)
	}
}

// TestZigSpecCallEdges pins down R-0005's most consequential miss: CallRe
// was nil, so zero ECall edges were ever emitted for Zig regardless of
// content — impact BFS (`get`/depth=N) was a total no-op, not a partial
// gap.
func TestZigSpecCallEdges(t *testing.T) {
	p := SpecParser{S: zigSpec}
	src := []byte(`pub fn add(a: i32, b: i32) i32 {
    return a + b;
}

const Counter = struct {
    count: i32 = 0,

    pub fn init() Counter {
        return Counter{ .count = 0 };
    }
};

pub fn main() !void {
    var c = Counter.init();
    const sum = add(1, 2);
    _ = c;
    _ = sum;
}
`)
	pr, err := p.Parse("app.zig", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var mainID graph.NodeID
	for _, n := range pr.Nodes {
		if n.ID == "zig:app.main" {
			mainID = n.ID
		}
	}
	if mainID == "" {
		t.Fatalf("zig:app.main missing, got %+v", pr.Nodes)
	}
	callees := map[graph.NodeID]bool{}
	for _, e := range pr.Edges {
		if e.Src == mainID && e.Kind == graph.ECall {
			callees[e.Dst] = true
		}
	}
	for _, want := range []graph.NodeID{"zig:app.init", "zig:app.add"} {
		if !callees[want] {
			t.Errorf("main() missing call edge to %s; got callees %v", want, callees)
		}
	}
}

// TestZigSpecNegativeLines pins down that a plain `const x = 5;`, a `fn`
// type appearing as a mid-line parameter (anchored ^ excludes it since the
// line itself starts with `comptime`, not `fn`), and a call never mint
// nodes.
func TestZigSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: zigSpec}
	pr, err := p.Parse("neg.zig", []byte(`const x = 5;
comptime f: fn () void,
callFoo();
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestZigSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangZig {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangZig — zigSpec not registered via init()")
	}
}

func TestZigSpecDeterministic(t *testing.T) {
	p := SpecParser{S: zigSpec}
	pr1, err := p.Parse("pkg/app.zig", zigSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.zig", zigSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
