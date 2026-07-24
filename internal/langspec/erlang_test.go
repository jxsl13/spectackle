package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// erlangSrc exercises every erlangSpec Def (the -module attribute and two
// column-0 function clause heads) plus negative lines that must NOT mint
// nodes: -export, an indented pseudo clause head, and a comment.
var erlangSrc = []byte(`-module(app).
-export([run/1, helper/2]).

run(X) ->
    X.

helper(X, Y) ->
    X + Y.

  foo(X) ->
    X.

% foo() ->
`)

func TestErlangSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	if p.Lang() != graph.LangErl {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangErl)
	}
	if got, want := p.Extensions(), []string{".erl", ".hrl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestErlangSpecNodes(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("pkg/app.erl", erlangSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"erl:app.app":    {graph.KType, 1},
		"erl:app.run":    {graph.KFunc, 4},
		"erl:app.helper": {graph.KFunc, 7},
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
		if n.Lang != graph.LangErl {
			t.Errorf("%s Lang = %v, want erl", id, n.Lang)
		}
		if n.File != "pkg/app.erl" {
			t.Errorf("%s File = %q, want pkg/app.erl", id, n.File)
		}
	}
}

// TestErlangSpecNegativeLines pins down that -export(...), an indented
// pseudo function-head line (the "case clause" shape called out in the
// brief), and a "% foo() ->" comment never mint nodes.
func TestErlangSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("neg.erl", []byte(`-export([run/1, helper/2]).
  foo(X) ->
    X.
% foo() ->
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestErlangSpecMultiClause(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	// Two clauses of the same function `foo/1` at column 0: the parser
	// mints a KFunc node per clause head line (both minting the same
	// "erl:clauses.foo" ID) — disambiguation via "~2", "~3", ... suffixes
	// is the indexer's job (internal/ids), not SpecParser.Parse's, so both
	// raw entries are expected to survive with the same ID here.
	pr, err := p.Parse("clauses.erl", []byte(`-module(m).

foo(0) ->
    zero;
foo(N) ->
    N.
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3 (module + 2 foo clauses): %+v", len(pr.Nodes), pr.Nodes)
	}
	var fooLines []int
	for _, n := range pr.Nodes {
		if n.ID == "erl:clauses.foo" {
			if n.Kind != graph.KFunc {
				t.Errorf("erl:clauses.foo Kind = %v, want KFunc", n.Kind)
			}
			fooLines = append(fooLines, n.Line)
		}
	}
	if want := []int{3, 5}; !reflect.DeepEqual(fooLines, want) {
		t.Errorf("erl:clauses.foo clause lines = %v, want %v", fooLines, want)
	}
}

func TestErlangSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangErl {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangErl — erlangSpec not registered via init()")
	}
}

func TestErlangSpecDeterministic(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr1, err := p.Parse("pkg/app.erl", erlangSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.erl", erlangSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
