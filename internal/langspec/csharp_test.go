package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
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
		Kind graph.NodeKind
		Line int
	}{
		"cs:App.Foo":    {graph.KType, 1},
		"cs:App.Main":   {graph.KFunc, 4},
		"cs:App.Count":  {graph.KFunc, 10}, // method `Count()`, not the field
		"cs:App.Get":    {graph.KFunc, 14}, // generic method `Get<T>(...)`
		"cs:App.Square": {graph.KFunc, 18}, // expression-bodied `=> x * x;`
		"cs:App.IShape": {graph.KType, 21},
		"cs:App.Area":   {graph.KFunc, 22}, // interface method decl ending `;`
		"cs:App.Rect":   {graph.KType, 25},
		"cs:App.Coord":  {graph.KType, 29}, // primary-constructor record
		"cs:App.Color":  {graph.KType, 31},
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
