package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
)

// --- ruby fixture ---------------------------------------------------------

var rubySrc = []byte(`class Foo < Base
  def initialize(x)
    @x = x
  end

  def valid?
    @x > 0
  end

  def save!
    persist(@x)
  end

  def self.build(x)
    new(x)
  end
end

module Greeter
  def self.hello(name)
    "hello #{name}"
  end
end

def top_level(x)
  x * 2
end
`)

func TestRubySpecLangExtensions(t *testing.T) {
	p := SpecParser{S: rubySpec}
	if p.Lang() != graph.Lang("rb") {
		t.Errorf("Lang() = %v, want rb", p.Lang())
	}
	if got, want := p.Extensions(), []string{".rb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestRubySpecNodes(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("pkg/app.rb", rubySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"rb:app.Foo":        {graph.KType, 1},
		"rb:app.initialize": {graph.KFunc, 2},
		"rb:app.valid?":     {graph.KFunc, 6},
		"rb:app.save!":      {graph.KFunc, 10},
		"rb:app.build":      {graph.KFunc, 14},
		"rb:app.Greeter":    {graph.KType, 19},
		"rb:app.hello":      {graph.KFunc, 20},
		"rb:app.top_level":  {graph.KFunc, 25},
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
		if n.Lang != graph.Lang("rb") {
			t.Errorf("%s Lang = %v, want rb", id, n.Lang)
		}
	}
}

// rubyNegativeSrc proves lines that merely resemble a def/class (block
// terminators, plain method calls, keyword-only lines) never mint a node:
// `end` never matches `def`/`class`/`module`, and a bare call like
// `initialize(x)` isn't preceded by the `def` keyword.
var rubyNegativeSrc = []byte(`class Widget
  def render
    initialize(1)
    build(2)
  end
end
`)

func TestRubySpecNegatives(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("pkg/widget.rb", rubyNegativeSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]bool{
		"rb:widget.Widget": true,
		"rb:widget.render": true,
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d (only class Widget + def render, no 'end' or call lines): %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id := range want {
		if _, ok := byID[id]; !ok {
			t.Errorf("expected node %s missing, got %+v", id, pr.Nodes)
		}
	}
	if _, ok := byID["rb:widget.initialize"]; ok {
		t.Error("plain call line 'initialize(1)' must not mint a node")
	}
	if _, ok := byID["rb:widget.build"]; ok {
		t.Error("plain call line 'build(2)' must not mint a node")
	}
}

func TestRubySpecDeterministic(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr1, err := p.Parse("pkg/app.rb", rubySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/app.rb", rubySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

func TestRubySpecInRegistry(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("rb") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain an rb parser; rubySpec not registered")
	}
}
