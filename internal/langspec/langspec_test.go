package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/index"
	"github.com/jxsl13/spectacle/internal/resolve"
	"github.com/jxsl13/spectacle/internal/store"
)

// --- python fixture ---------------------------------------------------

var pythonSrc = []byte(`def top_level(x):
    return x

class Foo:
    def method(self, y):
        return y


def another():
    pass
`)

func nodesByID(pr index.ParseResult) map[graph.NodeID]graph.Node {
	m := map[graph.NodeID]graph.Node{}
	for _, n := range pr.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestPythonSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: pythonSpec}
	if p.Lang() != graph.LangPy {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangPy)
	}
	if got, want := p.Extensions(), []string{".py"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestPythonSpecNodes(t *testing.T) {
	p := SpecParser{S: pythonSpec}
	pr, err := p.Parse("pkg/app.py", pythonSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"py:app.top_level": {graph.KFunc, 1},
		"py:app.Foo":       {graph.KType, 4},
		"py:app.method":    {graph.KFunc, 5},
		"py:app.another":   {graph.KFunc, 9},
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
		if n.Lang != graph.LangPy {
			t.Errorf("%s Lang = %v, want py", id, n.Lang)
		}
		if n.File != "pkg/app.py" {
			t.Errorf("%s File = %q, want pkg/app.py", id, n.File)
		}
	}
}

// --- javascript fixture -------------------------------------------------

var javascriptSrc = []byte(`export function run(x) {
  return x;
}

class Foo extends Base {
  greet() {}
}

export const add = (a, b) => a + b;

async function fetchData() {
  return null;
}

const mul = async (a, b) => a * b;

let square = x => x * x;
`)

func TestJavascriptSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: javascriptSpec}
	if p.Lang() != graph.LangJS {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangJS)
	}
	if got, want := p.Extensions(), []string{".js", ".mjs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestJavascriptSpecNodes(t *testing.T) {
	p := SpecParser{S: javascriptSpec}
	pr, err := p.Parse("pkg/app.js", javascriptSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"js:app.run":       {graph.KFunc, 1},
		"js:app.Foo":       {graph.KType, 5},
		"js:app.add":       {graph.KFunc, 9},
		"js:app.fetchData": {graph.KFunc, 11},
		"js:app.mul":       {graph.KFunc, 15},
		"js:app.square":    {graph.KFunc, 17},
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
		if n.Lang != graph.LangJS {
			t.Errorf("%s Lang = %v, want js", id, n.Lang)
		}
	}
}

// --- cross-cutting: no edges, determinism, hash, All() -------------------

// TestSpecParserNoEdgesWithoutCallRe is the framework-default half of
// LSP-001: every registered Spec that leaves CallRe unset — every language
// except C/C++ as of T-0048 — must never emit edges, regardless of source
// content. This is what "CallRe unset = zero behavior change" (see
// langspec.go's Spec.CallRe doc) actually guarantees.
func TestSpecParserNoEdgesWithoutCallRe(t *testing.T) {
	for _, p := range All() {
		sp, ok := p.(SpecParser)
		if !ok || sp.S.CallRe != nil {
			continue
		}
		pr, err := p.Parse("x", []byte("def f():\n  pass\n"))
		if err != nil {
			t.Fatalf("%v Parse: %v", p.Lang(), err)
		}
		if len(pr.Edges) != 0 {
			t.Errorf("%v Parse must not emit edges with CallRe unset, got %+v", p.Lang(), pr.Edges)
		}
	}
}

// TestSpecParserCallEdgesWithCallRe is LSP-001's positive sibling: a Spec
// that does set CallRe emits ECall edges from a Def's brace-counted body
// span. cSpec is the reference (see c_test.go/cpp_test.go for exhaustive
// per-language coverage); this test only proves the framework wiring itself.
func TestSpecParserCallEdgesWithCallRe(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", []byte("static void helper(void) {\n    other();\n}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 1 {
		t.Fatalf("want exactly 1 ECall edge, got %d: %+v", len(pr.Edges), pr.Edges)
	}
	e := pr.Edges[0]
	if e.Src != "c:helper" || e.Dst != "c:other" || e.Kind != graph.ECall {
		t.Errorf("edge = %+v, want c:helper -ECall-> c:other", e)
	}
	if e.File != "x.c" || e.Line != 2 {
		t.Errorf("edge File/Line = %q/%d, want x.c/2", e.File, e.Line)
	}
}

func TestSpecParserDeterministic(t *testing.T) {
	p := SpecParser{S: pythonSpec}
	pr1, err := p.Parse("pkg/app.py", pythonSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.py", pythonSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestSpecParserHashMatchesContent(t *testing.T) {
	p := SpecParser{S: javascriptSpec}
	pr, err := p.Parse("pkg/app.js", javascriptSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pr.Hash == ([32]byte{}) {
		t.Error("Hash is zero, want sha256 of source content")
	}
}

func TestAllReturnsOneParserPerRegisteredSpec(t *testing.T) {
	parsers := All()
	// One parser per registered spec, no fixed count: languages register
	// themselves via init() in their own file, so this test must not pin
	// the roster — only the invariants (1:1 with registry, distinct langs,
	// and the two reference specs present).
	if len(parsers) != len(registry) {
		t.Fatalf("All() returned %d parsers, want %d (one per registered spec)", len(parsers), len(registry))
	}
	langs := map[graph.Lang]bool{}
	for _, p := range parsers {
		langs[p.Lang()] = true
	}
	if len(langs) != len(parsers) {
		t.Errorf("All() has duplicate langs: %v", langs)
	}
	if !langs[graph.LangPy] || !langs[graph.LangJS] {
		t.Errorf("All() langs = %v, want py and js present", langs)
	}
}

// --- e2e: run the real indexing pipeline over a temp tree ----------------

func TestIndexAllPyAndJS(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.py"), pythonSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), javascriptSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	pyNode, ok := g.Node("py:app.top_level")
	if !ok {
		t.Fatal("node py:app.top_level missing after IndexAll")
	}
	if pyNode.Kind != graph.KFunc || pyNode.Lang != graph.LangPy {
		t.Errorf("py:app.top_level = %+v, want Kind=KFunc Lang=py", pyNode)
	}

	jsNode, ok := g.Node("js:app.run")
	if !ok {
		t.Fatal("node js:app.run missing after IndexAll")
	}
	if jsNode.Kind != graph.KFunc || jsNode.Lang != graph.LangJS {
		t.Errorf("js:app.run = %+v, want Kind=KFunc Lang=js", jsNode)
	}
}
