package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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
		Kind    graph.NodeKind
		Line    int
		EndLine int // 0 means "same as Line"
	}{
		"java:App.Foo":   {graph.KType, 1, 0},
		"java:App.main":  {graph.KFunc, 4, 8},   // brace-counted body now that CallRe is wired (R-0005)
		"java:App.count": {graph.KFunc, 10, 12}, // method `count()`, not the field
		"java:App.risky": {graph.KFunc, 14, 16},
		"java:App.Shape": {graph.KType, 19, 0},
		"java:App.Color": {graph.KType, 23, 0},
		"java:App.Point": {graph.KType, 27, 0},
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

// javaGapSrc reproduces (in miniature) the R-0005 gap-fixture constructs
// previously missed by javaSpec (findings/java.md, scratch fixture
// gap-java/Calculator.java): a constructor, a generic method, a multi-line
// signature, an interface default method, and the intra-file calls that
// exercise the CallRe wiring fixing both call edges and EndLine spans.
var javaGapSrc = []byte(`public class Calculator {

    private int base;

    public Calculator(int base) {
        this.base = base;
    }

    public int add(int a, int b) {
        return a + b + base;
    }

    public int addAndDouble(int a, int b) {
        return doubleIt(add(a, b));
    }

    private int doubleIt(int x) {
        return x * 2;
    }

    public <T> T identity(T value) {
        return value;
    }

    public int longSum(
            int a,
            int b,
            int c) {
        return a + b + c;
    }

    interface Greeter {
        String greet(String name);

        default String hello() {
            return greet("world");
        }
    }
}
`)

// TestJavaSpecGapFixes pins down the R-0005 [high]/[medium] fixes: the
// constructor Def (which collides in ID with the class node itself — the
// indexer's "~2" disambiguation is out of scope here, so both raw entries
// are checked directly, mirroring TestErlangSpecFunctionClauseCollision's
// pattern), the generic-method Def, the multi-line-signature Def, the
// interface default-method Def, and CallRe-driven edges/EndLine spans.
func TestJavaSpecGapFixes(t *testing.T) {
	p := SpecParser{S: javaSpec}
	pr, err := p.Parse("pkg/Calculator.java", javaGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Constructor: collides in ID with the class node (both are
	// "java:Calculator.Calculator"), so find both by kind+line directly.
	var gotClass, gotCtor bool
	for _, n := range pr.Nodes {
		if n.ID != "java:Calculator.Calculator" {
			continue
		}
		switch {
		case n.Kind == graph.KType && n.Line == 1:
			gotClass = true
		case n.Kind == graph.KFunc && n.Line == 5:
			gotCtor = true
			if n.EndLine != 7 {
				t.Errorf("constructor EndLine = %d, want 7", n.EndLine)
			}
		}
	}
	if !gotClass {
		t.Error("class Calculator (KType) missing")
	}
	if !gotCtor {
		t.Errorf("constructor `public Calculator(int base) {` (KFunc) missing, got %+v", pr.Nodes)
	}

	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"java:Calculator.add":          {graph.KFunc, 9, 11},
		"java:Calculator.addAndDouble": {graph.KFunc, 13, 15}, // calls doubleIt, add
		"java:Calculator.doubleIt":     {graph.KFunc, 17, 19},
		"java:Calculator.identity":     {graph.KFunc, 21, 23}, // generic method `<T>`
		"java:Calculator.longSum":      {graph.KFunc, 25, 30}, // multi-line signature
		"java:Calculator.Greeter":      {graph.KType, 32, 32},
		"java:Calculator.hello":        {graph.KFunc, 35, 37}, // interface default method, calls greet
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

	// The `greet` abstract interface method decl (no body, ends in `;`)
	// stays unfixed: a [low] finding, deliberately not chased.
	if _, ok := byID["java:Calculator.greet"]; ok {
		t.Error("bare interface method decl `String greet(String name);` unexpectedly minted a node (was deliberately left as a [low], not-chased gap)")
	}

	// CallRe-driven call edges (R-0005 EDGES: previously zero, always).
	wantEdges := map[[2]graph.NodeID]bool{
		{"java:Calculator.addAndDouble", "java:Calculator.doubleIt"}: false,
		{"java:Calculator.addAndDouble", "java:Calculator.add"}:      false,
		{"java:Calculator.hello", "java:Calculator.greet"}:           false,
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
