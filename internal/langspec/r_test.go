package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
	"github.com/jxsl13/spectackle/internal/index"
	"github.com/jxsl13/spectackle/internal/resolve"
	"github.com/jxsl13/spectackle/internal/store"
)

// rSrc exercises both rSpec KFunc Defs (`<-` and `=` assignment forms,
// including a dot-name, which is idiomatic in R) plus negative lines that
// must NOT mint nodes: a plain variable assignment, an anonymous function
// passed mid-line to lapply, and a commented-out definition.
var rSrc = []byte(`run <- function(x) {
  x
}

my.func <- function(x, y) {
  x + y
}

helper = function(z) {
  z
}

x <- 5
lapply(v, function(i) i)
# f <- function() 1
`)

func TestRSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: rSpec}
	if p.Lang() != graph.LangR {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangR)
	}
	if got, want := p.Extensions(), []string{".r"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestRSpecNodes(t *testing.T) {
	p := SpecParser{S: rSpec}
	pr, err := p.Parse("pkg/app.r", rSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	// EndLine now bounds the real body: rSpec sets CallRe (T-0123), so
	// SpecParser brace-counts a def's body instead of collapsing EndLine
	// onto the def line — R's function bodies are brace-delimited.
	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"r:app.run":     {graph.KFunc, 1, 3},
		"r:app.my.func": {graph.KFunc, 5, 7},
		"r:app.helper":  {graph.KFunc, 9, 11},
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
		if n.Line != w.Line || n.EndLine != w.EndLine {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, w.EndLine)
		}
		if n.Lang != graph.LangR {
			t.Errorf("%s Lang = %v, want r", id, n.Lang)
		}
		if n.File != "pkg/app.r" {
			t.Errorf("%s File = %q, want pkg/app.r", id, n.File)
		}
	}
}

// TestRSpecNegativeLines pins down that a plain variable assignment, an
// anonymous function passed mid-line, and a commented-out definition never
// mint nodes.
func TestRSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: rSpec}
	pr, err := p.Parse("neg.r", []byte(`x <- 5
lapply(v, function(i) i)
# f <- function() 1
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestRSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangR {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangR — rSpec not registered via init()")
	}
}

func TestRSpecDeterministic(t *testing.T) {
	p := SpecParser{S: rSpec}
	pr1, err := p.Parse("pkg/app.r", rSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.r", rSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestRSpecIndexAllUppercaseExtension proves, via the real indexing
// pipeline (not just SpecParser.Parse directly), that a source file with an
// uppercase ".R" extension is routed to rSpec even though rSpec.Exts lists
// only the lowercase ".r": index.LangOf lowercases unknown-case extensions
// before its extLang lookup, and index.indexer.IndexAll independently
// lowercases the extension before consulting the parsers-by-extension map
// built from Extensions() (see internal/index/indexer.go and
// internal/index/langs.go) — so registering only ".r" in Exts is sufficient
// for end-to-end uppercase routing, which is exactly what this test pins
// down.
func TestRSpecIndexAllUppercaseExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "App.R"), rSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	n, ok := g.Node("r:App.run")
	if !ok {
		t.Fatal("node r:App.run missing after IndexAll over an uppercase .R file")
	}
	if n.Kind != graph.KFunc || n.Lang != graph.LangR {
		t.Errorf("r:App.run = %+v, want Kind=KFunc Lang=r", n)
	}
}
