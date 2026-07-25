package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// --- python fixture -----------------------------------------------------
//
// pythonSpec was one of two registered Specs shipping without a test file at
// all (javascript was the other, see javascript_test.go). The fixture below
// carries every construct R-0005 found missing, so a regression shows up as
// a named failure rather than as a silently emptier graph.

var pythonSpecSrc = []byte(`def run(x):
    return x


async def fetch(url):
    return url


class Outer:
    def method(self, y):
        return y

    async def worker(self, job):
        return job

    class Inner:
        def deep(self):
            return 1


def multi_line_sig(
    first,
    second,
):
    return first
`)

func TestPythonSpecLangAndExtensions(t *testing.T) {
	p := SpecParser{S: pythonSpec}
	if p.Lang() != graph.LangPy {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangPy)
	}
	if got, want := p.Extensions(), []string{".py"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

// TestPythonSpecR0005Constructs covers the three R-0005 fixes together: `async def` at
// any indentation (previously unmatched entirely, since the Def required the
// line to start with literal `def`), a nested `class Inner` (the class Def
// used to be anchored at column zero), and the parameter list feeding Sig
// (no Python Def set Sig at all, so Node.Sig was always empty).
func TestPythonSpecR0005Constructs(t *testing.T) {
	p := SpecParser{S: pythonSpec}
	pr, err := p.Parse("pkg/app.py", pythonSpecSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
		Sig  string
	}{
		"py:app.run":            {graph.KFunc, 1, "x"},
		"py:app.fetch":          {graph.KFunc, 5, "url"},
		"py:app.Outer":          {graph.KType, 9, ""},
		"py:app.method":         {graph.KFunc, 10, "self, y"},
		"py:app.worker":         {graph.KFunc, 13, "self, job"},
		"py:app.Inner":          {graph.KType, 16, ""},
		"py:app.deep":           {graph.KFunc, 17, "self"},
		"py:app.multi_line_sig": {graph.KFunc, 21, ""},
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
		if n.Line != w.Line {
			t.Errorf("%s Line = %d, want %d", id, n.Line, w.Line)
		}
		if n.Sig != w.Sig {
			t.Errorf("%s Sig = %q, want %q", id, n.Sig, w.Sig)
		}
		if n.Lang != graph.LangPy {
			t.Errorf("%s Lang = %v, want py", id, n.Lang)
		}
		if n.File != "pkg/app.py" {
			t.Errorf("%s File = %q, want pkg/app.py", id, n.File)
		}
	}
}

// TestPythonSpecCallReRemainsNilAndWhy pins a deliberate non-fix, so the
// next reader does not "repair" it by hand. Python delimits bodies by
// indentation, and every body-span mechanism this framework has counts
// tokens: cspan.Span counts braces, cspan.KeywordSpan counts opener/closer
// keywords. Neither can bound a Python body, and without a bounded body a
// call regex would either match nothing or match every line in the file.
// Wiring CallRe here would therefore be a silent correctness regression, not
// a feature — the honest fix is an indentation-aware span in the shared
// engine, which is a separate change to a file this Spec does not own.
//
// Consequence, stated so it is not mistaken for an oversight: Python nodes
// keep EndLine == Line and .py files produce no ECall edges.
func TestPythonSpecCallReRemainsNilAndWhy(t *testing.T) {
	if pythonSpec.CallRe != nil {
		t.Fatal("pythonSpec must leave CallRe nil: indentation-delimited bodies " +
			"cannot be bounded by cspan.Span or cspan.KeywordSpan, so a call " +
			"regex here would be unbounded — see this test's doc comment")
	}
	if pythonSpec.EndSpan != nil {
		t.Fatal("pythonSpec must leave EndSpan nil for the same reason: its " +
			"open/close keywords do not exist in Python's grammar")
	}

	pr, err := SpecParser{S: pythonSpec}.Parse("pkg/app.py", pythonSpecSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Fatalf("Parse emitted %d edges with CallRe nil: %+v", len(pr.Edges), pr.Edges)
	}
	for _, n := range pr.Nodes {
		if n.EndLine != n.Line {
			t.Errorf("%s EndLine = %d, want it to equal Line %d while CallRe is nil",
				n.ID, n.EndLine, n.Line)
		}
	}
}
