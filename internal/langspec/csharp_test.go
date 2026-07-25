package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// csharpSrc exercises every csharpSpec Def (class/interface/struct/record/
// enum, and the method heuristic including a generic method and an
// expression-bodied method) plus negative lines that must NOT mint nodes:
// a field decl and an if/return inside a method.
var csharpSrc = []byte(`public class Foo {
    private int count;

    public static void Main(string[] args) {
        if (count > 0) {
            Console.WriteLine("positive");
        }
    }

    private int Count() {
        return count;
    }

    public T Get<T>(int id) {
        return default(T);
    }

    public int Square(int x) => x * x;
}

public interface IShape {
    public double Area();
}

public struct Rect {
    public int Width;
}

public record Coord(int X, int Y);

public enum Color {
    Red, Green, Blue
}
`)

func TestCSharpSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: csharpSpec}
	if p.Lang() != graph.Lang("cs") {
		t.Errorf("Lang() = %v, want cs", p.Lang())
	}
	if got, want := p.Extensions(), []string{".cs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestCSharpSpecNodes(t *testing.T) {
	p := SpecParser{S: csharpSpec}
	pr, err := p.Parse("pkg/App.cs", csharpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int // 0 means "same as Line"
	}{
		"cs:App.Foo":    {graph.KType, 1, 0},
		"cs:App.Main":   {graph.KFunc, 4, 8},   // brace-counted body now that CallRe is wired (R-0005)
		"cs:App.Count":  {graph.KFunc, 10, 12}, // method `Count()`, not the field
		"cs:App.Get":    {graph.KFunc, 14, 16}, // generic method `Get<T>(...)`
		"cs:App.Square": {graph.KFunc, 18, 0},  // expression-bodied `=> x * x;`: no brace, span unchanged
		"cs:App.IShape": {graph.KType, 21, 0},
		"cs:App.Area":   {graph.KFunc, 22, 0}, // interface method decl ending `;`: no brace, span unchanged
		"cs:App.Rect":   {graph.KType, 25, 0},
		"cs:App.Coord":  {graph.KType, 29, 0}, // primary-constructor record
		"cs:App.Color":  {graph.KType, 31, 0},
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		wantEnd := w.EndLine
		if wantEnd == 0 {
			wantEnd = w.Line
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != wantEnd {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, wantEnd)
		}
		if n.Lang != graph.Lang("cs") {
			t.Errorf("%s Lang = %v, want cs", id, n.Lang)
		}
		if n.File != "pkg/App.cs" {
			t.Errorf("%s File = %q, want pkg/App.cs", id, n.File)
		}
	}
}

// TestCSharpSpecNegativeLines pins down that a field decl (`private int
// count;`, never reaches a `(` before `;`) and if/return statements (no
// leading modifier keyword) never mint nodes for the method Def.
func TestCSharpSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: csharpSpec}
	pr, err := p.Parse("Neg.cs", []byte(`class Neg {
    private int count;

    void Run() {
        if (count > 0) {
            return;
        }
    }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["cs:Neg.count"]; ok {
		t.Error("field decl `private int count;` must not mint a node")
	}
	if _, ok := byID["cs:Neg.Run"]; ok {
		t.Error("void Run() {  with no modifier must not match the pragmatic method heuristic")
	}
	// Only the class itself should mint a node: `Run` has no modifier
	// keyword, and if/return are correctly excluded.
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only class Neg): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["cs:Neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
	}
}

// csharpGapSrc reproduces the R-0005 gap-fixture constructs previously
// missed by csharpSpec (findings/csharp.md, scratch fixture
// gap-csharp/Sample.cs): Allman-style brace placement (the idiomatic
// .NET/Visual-Studio default), a constructor, interface members and an
// explicit interface implementation method (neither carries a modifier
// keyword), a local function, a multi-line signature, and the intra-file
// calls that exercise the CallRe wiring fixing both call edges and
// EndLine spans.
var csharpGapSrc = []byte(`public interface IShape
{
    double Area();
}

public interface IPrintable
{
    void Print();
}

public class Circle : IShape, IPrintable
{
    private readonly double radius;

    public Circle(double radius)
    {
        this.radius = radius;
    }

    public double Area()
    {
        return ComputeArea(radius);
    }

    private double ComputeArea(double r)
    {
        double Square(double v) => v * v;
        return Square(r) * 3;
    }

    void IPrintable.Print()
    {
        Console.WriteLine(Area());
    }
}

public static class MathUtils
{
    public static Task<int> FetchAsync(
        string url,
        int retries)
    {
        return null;
    }
}
`)

// TestCSharpSpecGapFixes pins down the R-0005 [high]/[medium] fixes: Allman
// brace placement, the constructor Def, interface-member and explicit-
// interface-implementation Defs (both modifier-less), the local-function
// Def, the multi-line-signature alternative, and CallRe-driven
// edges/EndLine spans.
func TestCSharpSpecGapFixes(t *testing.T) {
	p := SpecParser{S: csharpSpec}
	pr, err := p.Parse("pkg/gap.cs", csharpGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// "Area" and "Print" each collide in ID between an interface member
	// decl and its implementation (file-stem qualification has no
	// class-scoping — a documented, out-of-scope-here limitation, same as
	// TestJavaSpecGapFixes's class/constructor collision), so find each by
	// kind+line directly instead of relying on a byID map overwrite.
	find := func(id graph.NodeID, line int) (graph.Node, bool) {
		for _, n := range pr.Nodes {
			if n.ID == id && n.Line == line {
				return n, true
			}
		}
		return graph.Node{}, false
	}

	if _, ok := find("cs:gap.Area", 3); !ok {
		t.Errorf("interface member `double Area();` (no modifier) missing, got %+v", pr.Nodes)
	}
	if n, ok := find("cs:gap.Area", 20); !ok {
		t.Errorf("Allman-brace `public double Area()` missing, got %+v", pr.Nodes)
	} else if n.EndLine != 23 {
		t.Errorf("cs:gap.Area@20 EndLine = %d, want 23", n.EndLine)
	}
	if _, ok := find("cs:gap.Print", 8); !ok {
		t.Errorf("interface member `void Print();` (no modifier) missing, got %+v", pr.Nodes)
	}
	if n, ok := find("cs:gap.Print", 31); !ok {
		t.Errorf("explicit interface implementation `void IPrintable.Print()` missing, got %+v", pr.Nodes)
	} else if n.EndLine != 34 {
		t.Errorf("cs:gap.Print@31 EndLine = %d, want 34", n.EndLine)
	}
	if n, ok := find("cs:gap.Circle", 15); !ok {
		t.Errorf("constructor `public Circle(double radius)` (Allman) missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KFunc || n.EndLine != 18 {
		t.Errorf("cs:gap.Circle@15 Kind/EndLine = %v/%d, want KFunc/18", n.Kind, n.EndLine)
	}
	if _, ok := find("cs:gap.Circle", 11); !ok {
		t.Error("class Circle (KType) missing")
	}

	byID := nodesByID(pr)
	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"cs:gap.IShape":      {graph.KType, 1, 1},
		"cs:gap.IPrintable":  {graph.KType, 6, 6},
		"cs:gap.ComputeArea": {graph.KFunc, 25, 29},
		"cs:gap.Square":      {graph.KFunc, 27, 27}, // expression-bodied local function, no brace
		"cs:gap.MathUtils":   {graph.KType, 37, 37},
		"cs:gap.FetchAsync":  {graph.KFunc, 39, 44}, // multi-line signature + Allman brace
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
	}

	wantEdges := map[[2]graph.NodeID]bool{
		{"cs:gap.Area", "cs:gap.ComputeArea"}:   false,
		{"cs:gap.ComputeArea", "cs:gap.Square"}: false,
		{"cs:gap.Print", "cs:gap.Area"}:         false,
	}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge Kind = %v, want ECall", e.Kind)
		}
		key := [2]graph.NodeID{e.Src, e.Dst}
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
		}
	}
	for k, seen := range wantEdges {
		if !seen {
			t.Errorf("missing call edge %s -> %s, got edges %+v", k[0], k[1], pr.Edges)
		}
	}
}

func TestCSharpSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("cs") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"cs\") — csharpSpec not registered via init()")
	}
}

func TestCSharpSpecDeterministic(t *testing.T) {
	p := SpecParser{S: csharpSpec}
	pr1, err := p.Parse("pkg/App.cs", csharpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/App.cs", csharpSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
