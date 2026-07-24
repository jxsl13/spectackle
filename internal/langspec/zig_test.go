package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
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

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"zig:app.run":       {graph.KFunc, 1},
		"zig:app.add":       {graph.KFunc, 4},
		"zig:app.exported":  {graph.KFunc, 8},
		"zig:app.cImported": {graph.KFunc, 11},
		"zig:app.fast":      {graph.KFunc, 13},
		"zig:app.Point":     {graph.KType, 16},
		"zig:app.Color":     {graph.KType, 21},
		"zig:app.Handle":    {graph.KType, 26},
		"zig:app.Shape":     {graph.KType, 28},
		"zig:app.Packed":    {graph.KType, 33},
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
		if n.Lang != graph.LangZig {
			t.Errorf("%s Lang = %v, want zig", id, n.Lang)
		}
		if n.File != "pkg/app.zig" {
			t.Errorf("%s File = %q, want pkg/app.zig", id, n.File)
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
