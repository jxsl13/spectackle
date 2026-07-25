package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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

// TestErlangSpecGuardClause pins down R-0005's headline miss: a function
// clause head with a guard expression (`when ... ->`) — the old regex
// required `->` to land immediately after the closing `)`.
func TestErlangSpecGuardClause(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("app.erl", []byte(`classify(X) when X > 0 ->
    positive;
classify(0) ->
    zero;
classify(X) when X < 0 ->
    negative.
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// All three clauses (two guarded, one bare) mint the same collision-
	// prone ID, per this Def's documented multi-clause behavior — so check
	// pr.Nodes directly (nodesByID's map would only keep the last write)
	// exactly like TestErlangSpecMultiClause does.
	var lines []int
	for _, nd := range pr.Nodes {
		if nd.ID == "erl:app.classify" {
			if nd.Kind != graph.KFunc {
				t.Errorf("erl:app.classify Kind = %v, want KFunc", nd.Kind)
			}
			lines = append(lines, nd.Line)
		}
	}
	if want := []int{1, 3, 5}; !reflect.DeepEqual(lines, want) {
		t.Errorf("erl:app.classify clause lines = %v, want %v (got nodes %+v)", lines, want, pr.Nodes)
	}
}

// TestErlangSpecMultiLineHead pins down R-0005's headline miss: a function
// head whose args and `->` wrap to a later physical line — the old regex
// needed `)`+`->` on the SAME line as the name.
func TestErlangSpecMultiLineHead(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("app.erl", []byte(`combine(A,
        B) ->
    A + B.
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["erl:app.combine"]
	if !ok {
		t.Fatalf("erl:app.combine missing, got %+v", pr.Nodes)
	}
	if n.Kind != graph.KFunc || n.Line != 1 {
		t.Errorf("combine = %+v, want KFunc Line=1", n)
	}
}

// TestErlangSpecRecordType pins down R-0005's two headline misses:
// `-record(name, {fields}).` and `-type name() :: ...` declarations, the
// two core Erlang type-definition forms, previously had no Def at all.
func TestErlangSpecRecordType(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("app.erl", []byte(`-record(person, {name, age}).

-type option() :: {ok, term()} | {error, term()}.
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if n, ok := byID["erl:app.person"]; !ok {
		t.Errorf("erl:app.person missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("person Kind = %v, want KType", n.Kind)
	}
	if n, ok := byID["erl:app.option"]; !ok {
		t.Errorf("erl:app.option missing, got %+v", pr.Nodes)
	} else if n.Kind != graph.KType {
		t.Errorf("option Kind = %v, want KType", n.Kind)
	}
}

// TestErlangSpecNoCallEdges pins down that erlangSpec deliberately leaves
// CallRe nil (R-0005 scope: Erlang has no braces for cspan.Span to count) —
// zero edges are emitted even when a body plainly calls other local
// functions.
func TestErlangSpecNoCallEdges(t *testing.T) {
	p := SpecParser{S: erlangSpec}
	pr, err := p.Parse("app.erl", []byte(`run(X) ->
    Y = helper(X, X),
    classify(Y).

helper(X, Y) ->
    X + Y.
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("got %d edges, want 0 (CallRe deliberately unset for Erlang): %+v", len(pr.Edges), pr.Edges)
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
