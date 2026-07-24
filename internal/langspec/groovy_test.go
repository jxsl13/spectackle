package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// groovySrc exercises every groovySpec Def: a top-level dynamically typed
// `def` function, a class (KType) containing a statically typed modifier-led
// method and a `def` method, and a `trait` (KType).
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
		Kind graph.NodeKind
		Line int
	}{
		"groovy:App.buildGreeting": {graph.KFunc, 1},
		"groovy:App.Greeter":       {graph.KType, 5},
		"groovy:App.greet":         {graph.KFunc, 8},
		"groovy:App.helper":        {graph.KFunc, 12},
		"groovy:App.Flyable":       {graph.KType, 17},
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
		if n.Lang != graph.LangGroovy {
			t.Errorf("%s Lang = %v, want groovy", id, n.Lang)
		}
		if n.File != "pkg/App.groovy" {
			t.Errorf("%s File = %q, want pkg/App.groovy", id, n.File)
		}
	}
}

// TestGroovySpecNegativeLines pins down that a field decl (`private String
// prefix`, never reaches a `(` before end of line), an if-statement (`if
// (...) {`, no modifier keyword), and a bare call statement (`println
// foo()`, not preceded by `def` or a modifier+type) never mint nodes.
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
	if _, ok := byID["groovy:Neg.run"]; ok {
		t.Error("void run() {  with no modifier must not match the pragmatic typed-method heuristic")
	}
	if _, ok := byID["groovy:Neg.foo"]; ok {
		t.Error("`println foo()` call statement must not mint a node")
	}
	// Only the class itself should mint a node: `run` has no modifier, `if`
	// has no modifier/def, and `println foo()` is a call, not a def.
	if len(pr.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (only class Neg): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["groovy:Neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
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
