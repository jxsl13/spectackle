package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// groovySrc exercises every groovySpec Def: a top-level dynamically typed
// `def` function, a class (KType) containing a statically typed modifier-led
// method and a `def` method, and a `trait` (KType) containing a
// modifier-less (default-visibility) typed method — R-0005's biggest single
// miss, now fixed.
var groovySrc = []byte(`def buildGreeting(name) {
    return "Hello, $name"
}

class Greeter {
    private String prefix

    public String greet(String name) {
        return prefix + name
    }

    def helper(x) {
        return x * 2
    }
}

trait Flyable {
    void fly() {
        println 'flying'
    }
}
`)

func TestGroovySpecLangExtensions(t *testing.T) {
	p := SpecParser{S: groovySpec}
	if p.Lang() != graph.LangGroovy {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangGroovy)
	}
	if got, want := p.Extensions(), []string{".groovy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestGroovySpecNodes(t *testing.T) {
	p := SpecParser{S: groovySpec}
	pr, err := p.Parse("pkg/App.groovy", groovySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int // 0 means "same as Line"
	}{
		"groovy:App.buildGreeting": {graph.KFunc, 1, 3}, // brace-counted body now that CallRe is wired (R-0005)
		"groovy:App.Greeter":       {graph.KType, 5, 0},
		"groovy:App.greet":         {graph.KFunc, 8, 10},
		"groovy:App.helper":        {graph.KFunc, 12, 14},
		"groovy:App.Flyable":       {graph.KType, 17, 0},
		"groovy:App.fly":           {graph.KFunc, 18, 20}, // modifier-less typed method (R-0005)
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
		if n.Lang != graph.LangGroovy {
			t.Errorf("%s Lang = %v, want groovy", id, n.Lang)
		}
		if n.File != "pkg/App.groovy" {
			t.Errorf("%s File = %q, want pkg/App.groovy", id, n.File)
		}
	}
}

// TestGroovySpecNegativeLines pins down that a field decl (`private String
// prefix`, never reaches a `(` before end of line) and an if-statement
// (`if (prefix) {`, a single token before `(` — structurally disjoint from
// any real method/constructor shape, which always has a name immediately
// followed by `(` with no space) never mint nodes. `void run() {` (no
// modifier) now DOES mint a node — R-0005 [high]: Groovy's default
// visibility is `public`, so a modifier-less typed method is the idiomatic
// common case, not something to exclude (this test's expectation for `run`
// was proven wrong by that finding; see groovy.go's Def comment). A bare
// call statement written Groovy-command-style with no parens on the
// receiver (`println foo()`) is structurally identical to a body-less
// interface method decl (`double area()`), so that [medium] finding was
// deliberately left un-chased rather than reintroduce this false positive.
func TestGroovySpecNegativeLines(t *testing.T) {
	p := SpecParser{S: groovySpec}
	pr, err := p.Parse("Neg.groovy", []byte(`class Neg {
    private String prefix

    void run() {
        if (prefix) {
            println foo()
        }
    }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["groovy:Neg.prefix"]; ok {
		t.Error("field decl `private String prefix` must not mint a node")
	}
	if _, ok := byID["groovy:Neg.if"]; ok {
		t.Error("`if (prefix) {` control-flow line must not mint a node")
	}
	if _, ok := byID["groovy:Neg.foo"]; ok {
		t.Error("`println foo()` call statement must not mint a node")
	}
	// The class and `run` (now correctly matched as a modifier-less typed
	// method, R-0005) should mint nodes; `if` and `println foo()` still
	// correctly don't.
	if len(pr.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (class Neg + void run()): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["groovy:Neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
	}
	if _, ok := byID["groovy:Neg.run"]; !ok {
		t.Errorf("void run() {  (modifier-less typed method) missing, got %+v", pr.Nodes)
	}
}

// groovyGapSrc reproduces the R-0005 gap-fixture constructs previously
// missed by groovySpec (findings/groovy.md, scratch fixture
// gap-groovy/Payments.groovy): a constructor, a multi-line method
// signature, a static nested class, and the intra-file calls that exercise
// the CallRe wiring fixing both call edges and EndLine spans.
var groovyGapSrc = []byte(`class PaymentService {

    public PaymentService(String id) {
        this.id = id
    }

    public boolean refund(
            String reason,
            BigDecimal amount) {
        return charge(amount)
    }

    private boolean charge(BigDecimal amount) {
        return true
    }

    static class Inner {
        void process() {
            println "processing"
        }
    }
}
`)

// TestGroovySpecGapFixes pins down the R-0005 [high] fixes: the constructor
// Def, the multi-line-signature Def, the static-nested-class Def, and
// CallRe-driven edges/EndLine spans.
func TestGroovySpecGapFixes(t *testing.T) {
	p := SpecParser{S: groovySpec}
	pr, err := p.Parse("pkg/PaymentService.groovy", groovyGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Constructor: collides in ID with the class node (both are
	// "groovy:PaymentService.PaymentService"), so find both by kind+line
	// directly, mirroring TestJavaSpecGapFixes.
	var gotClass, gotCtor bool
	for _, n := range pr.Nodes {
		if n.ID != "groovy:PaymentService.PaymentService" {
			continue
		}
		switch {
		case n.Kind == graph.KType && n.Line == 1:
			gotClass = true
		case n.Kind == graph.KFunc && n.Line == 3:
			gotCtor = true
			if n.EndLine != 5 {
				t.Errorf("constructor EndLine = %d, want 5", n.EndLine)
			}
		}
	}
	if !gotClass {
		t.Error("class PaymentService (KType) missing")
	}
	if !gotCtor {
		t.Errorf("constructor `public PaymentService(String id) {` (KFunc) missing, got %+v", pr.Nodes)
	}

	byID := nodesByID(pr)
	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
	}{
		"groovy:PaymentService.refund":  {graph.KFunc, 7, 11}, // multi-line signature, calls charge
		"groovy:PaymentService.charge":  {graph.KFunc, 13, 15},
		"groovy:PaymentService.Inner":   {graph.KType, 17, 17}, // static nested class
		"groovy:PaymentService.process": {graph.KFunc, 18, 20},
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
		{"groovy:PaymentService.refund", "groovy:PaymentService.charge"}: false,
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

func TestGroovySpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangGroovy {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangGroovy — groovySpec not registered via init()")
	}
}

func TestGroovySpecDeterministic(t *testing.T) {
	p := SpecParser{S: groovySpec}
	pr1, err := p.Parse("pkg/App.groovy", groovySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/App.groovy", groovySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
