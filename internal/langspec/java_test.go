package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// javaSrc exercises every javaSpec Def (class/interface/enum/record, and
// the method heuristic including a throws clause) plus negative lines that
// must NOT mint nodes: a field decl and an if-statement.
var javaSrc = []byte(`public class Foo {
    private int count;

    public static void main(String[] args) {
        if (count > 0) {
            System.out.println("positive");
        }
    }

    private int count() {
        return count;
    }

    public void risky() throws Exception {
        throw new Exception();
    }
}

interface Shape {
    double area();
}

enum Color {
    RED, GREEN, BLUE
}

record Point(int x, int y) {
}
`)

func TestJavaSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: javaSpec}
	if p.Lang() != graph.Lang("java") {
		t.Errorf("Lang() = %v, want java", p.Lang())
	}
	if got, want := p.Extensions(), []string{".java"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestJavaSpecNodes(t *testing.T) {
	p := SpecParser{S: javaSpec}
	pr, err := p.Parse("pkg/App.java", javaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"java:App.Foo":   {graph.KType, 1},
		"java:App.main":  {graph.KFunc, 4},
		"java:App.count": {graph.KFunc, 10}, // method `count()`, not the field
		"java:App.risky": {graph.KFunc, 14},
		"java:App.Shape": {graph.KType, 19},
		"java:App.Color": {graph.KType, 23},
		"java:App.Point": {graph.KType, 27},
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
		if n.Lang != graph.Lang("java") {
			t.Errorf("%s Lang = %v, want java", id, n.Lang)
		}
		if n.File != "pkg/App.java" {
			t.Errorf("%s File = %q, want pkg/App.java", id, n.File)
		}
	}
}

// TestJavaSpecNegativeLines pins down that a field decl (`private int
// count;`, never reaches a `(` before `;`) and an if-statement (`if (...)
// {`, no leading modifier keyword) never mint nodes for the method Def.
func TestJavaSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: javaSpec}
	pr, err := p.Parse("Neg.java", []byte(`class Neg {
    private int count;

    void run() {
        if (count > 0) {
            return;
        }
        for (int i = 0; i < count; i++) {
            count++;
        }
    }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["java:Neg.count"]; ok {
		t.Error("field decl `private int count;` must not mint a node")
	}
	if _, ok := byID["java:Neg.run"]; ok {
		t.Error("void run() {  with no modifier must not match the pragmatic method heuristic")
	}
	// Only the class itself should mint a node: `run` has no modifier
	// keyword, and if/for/return are correctly excluded.
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only class Neg): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["java:Neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
	}
}

func TestJavaSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("java") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"java\") — javaSpec not registered via init()")
	}
}

func TestJavaSpecDeterministic(t *testing.T) {
	p := SpecParser{S: javaSpec}
	pr1, err := p.Parse("pkg/App.java", javaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/App.java", javaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
