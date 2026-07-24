package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// shellSrc exercises every shellSpec Def: POSIX `name() {`, bash
// `function name() {` (still matched by the POSIX Def since the `function`
// keyword prefix is optional there), and bash's parenthesis-less
// `function name {`.
var shellSrc = []byte(`foo() {
    echo "hi"
}

function bar() {
    echo "hi"
}

function baz {
    echo "hi"
}
`)

func TestShellSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: shellSpec}
	if p.Lang() != graph.LangSh {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangSh)
	}
	if got, want := p.Extensions(), []string{".sh", ".bash"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestShellSpecNodes(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"sh:app.foo": {graph.KFunc, 1},
		"sh:app.bar": {graph.KFunc, 5},
		"sh:app.baz": {graph.KFunc, 9},
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
		if n.Lang != graph.LangSh {
			t.Errorf("%s Lang = %v, want sh", id, n.Lang)
		}
		if n.File != "pkg/app.sh" {
			t.Errorf("%s File = %q, want pkg/app.sh", id, n.File)
		}
	}
}

// TestShellSpecNegativeLines pins down that command invocations, an
// if-statement, a subshell, and a case pattern never mint nodes.
func TestShellSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr, err := p.Parse("neg.sh", []byte(`echo foo
if [ -f myfile ]; then
    echo yes
fi
(cd /tmp)
case "$x" in
  foo)
    echo match
    ;;
esac
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestShellSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangSh {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangSh — shellSpec not registered via init()")
	}
}

func TestShellSpecDeterministic(t *testing.T) {
	p := SpecParser{S: shellSpec}
	pr1, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.sh", shellSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
