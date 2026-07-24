package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// perlSrc exercises every perlSpec Def: `sub` and `package` (both a plain
// name and a `::`-qualified name, which stays in the captured name).
var perlSrc = []byte(`package MyApp;

sub run {
    return 1;
}

package MyApp::Utils;

sub helper {
    return 2;
}
`)

func TestPerlSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: perlSpec}
	if p.Lang() != graph.LangPerl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangPerl)
	}
	if got, want := p.Extensions(), []string{".pl", ".pm"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestPerlSpecNodes(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"pl:app.MyApp":        {graph.KType, 1},
		"pl:app.run":          {graph.KFunc, 3},
		"pl:app.MyApp::Utils": {graph.KType, 7},
		"pl:app.helper":       {graph.KFunc, 9},
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
		if n.Lang != graph.LangPerl {
			t.Errorf("%s Lang = %v, want pl", id, n.Lang)
		}
		if n.File != "pkg/app.pl" {
			t.Errorf("%s File = %q, want pkg/app.pl", id, n.File)
		}
	}
}

// TestPerlSpecNegativeLines pins down that an anonymous sub assignment, a
// commented-out sub, and a plain call never mint nodes.
func TestPerlSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr, err := p.Parse("neg.pl", []byte(`my $f = sub {
    return 1;
};
# sub foo
foo();
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestPerlSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangPerl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangPerl — perlSpec not registered via init()")
	}
}

func TestPerlSpecDeterministic(t *testing.T) {
	p := SpecParser{S: perlSpec}
	pr1, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.pl", perlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
