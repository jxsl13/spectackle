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

// TestRubySpecNodes' EndLine expectations were updated by T-0119: rubySpec
// now sets CallRe + EndSpan, so every multi-line body here keyword-counts to
// its real span (KType defs — class/module — stay Line==EndLine, since only
// KFunc/KMethod are ever span-computed). The pre-T-0119 assertion (EndLine
// always equals Line, even for KFunc) was pinning down exactly the
// "span defect" finding this task fixes ([high]: "Every symbol's span is
// the single def/class line, never the real body extent"), so updating it
// here is the fix, not a regression.
func TestRubySpecNodes(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("pkg/app.rb", rubySrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind          graph.NodeKind
		Line, EndLine int
	}{
		"rb:app.Foo":        {graph.KType, 1, 1},
		"rb:app.initialize": {graph.KFunc, 2, 4},
		"rb:app.valid?":     {graph.KFunc, 6, 8},
		"rb:app.save!":      {graph.KFunc, 10, 12},
		"rb:app.build":      {graph.KFunc, 14, 16},
		"rb:app.Greeter":    {graph.KType, 19, 19},
		"rb:app.hello":      {graph.KFunc, 20, 22},
		"rb:app.top_level":  {graph.KFunc, 25, 27},
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
		if n.Lang != graph.Lang("rb") {
			t.Errorf("%s Lang = %v, want rb", id, n.Lang)
		}
	}
}

// rubyGapFixtureSrc reuses gap-ruby/sample.rb's operator-method, setter,
// namespaced-class, and call-edge constructs from R-0005's empirical
// findings — every one of these was a [high] miss before T-0119:
//   - `def <=>(other)`: operator method definitions never matched at all
//     (the old Name capture required `\w+`, and no operator is a word).
//   - `def radius=(value)`: setter methods minted under the wrong/colliding
//     name "radius" (the trailing `=` was outside the old capture).
//   - `class Circle::Metrics`: namespaced/reopened classes minted under
//     just "Circle" (the old capture stopped at the first `::`).
//   - `build_and_describe` calling `build_default_circle` and (dotted)
//     `Metrics.describe`: zero call edges were ever produced for Ruby.
var rubyGapFixtureSrc = []byte(`module Shapes
  class Base
    def area
      raise NotImplementedError
    end

    def <=>(other)
      area <=> other.area
    end
  end

  class Circle::Metrics
    def self.describe(circle)
      circle.area
    end
  end
end

def build_default_circle
  circle = Shapes::Circle.new("default", 2)
  circle.area
end

def build_and_describe
  circle = build_default_circle
  Shapes::Circle::Metrics.describe(circle)
end
`)

func TestRubySpecGapFixtureOperatorSetterNamespaceAndEdges(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("sample.rb", rubyGapFixtureSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	// Operator method: minted under its exact operator name, not dropped.
	spaceship, ok := byID["rb:sample.<=>"]
	if !ok {
		t.Fatalf("rb:sample.<=> missing (operator method def never matched), got %+v", pr.Nodes)
	}
	if spaceship.Line != 7 || spaceship.EndLine != 9 {
		t.Errorf("<=> Line/EndLine = %d/%d, want 7/9", spaceship.Line, spaceship.EndLine)
	}

	// Namespaced/reopened class: minted under its full "Circle::Metrics"
	// name, not truncated to "Circle" (which would collide with a real
	// top-level Circle class elsewhere).
	if _, ok := byID["rb:sample.Circle::Metrics"]; !ok {
		t.Errorf("rb:sample.Circle::Metrics missing (namespaced class truncated at first '::'), got %+v", pr.Nodes)
	}

	// Call edges: build_and_describe calls Shapes::Circle::Metrics.describe
	// (dotted — CallRe captures the bare trailing segment "describe", same
	// qualifier-stripping behavior as every other CallRe'd langspec Spec).
	// It also calls build_default_circle, but WITHOUT parens (Ruby allows
	// omitting `()` on a no-arg call) — CallRe, like every other langspec
	// Spec's, only recognizes `name(` call sites, so a paren-less call is
	// not detectable by this line-oriented regex mechanism; that's a
	// pre-existing, structural limitation of LSP-001 itself (shared by
	// every brace-language Spec too), not something T-0119 introduces or
	// is scoped to fix.
	found := false
	for _, e := range pr.Edges {
		if e.Src == "rb:sample.build_and_describe" && e.Dst == "rb:sample.describe" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing ECall edge rb:sample.build_and_describe -> rb:sample.describe, got edges %+v", pr.Edges)
	}
}

// rubySetterSrc pins down the setter-method fix in isolation: `radius=`
// must mint under its own distinct name, not collide with a getter named
// `radius`.
var rubySetterSrc = []byte(`class Circle
  def radius
    @radius
  end

  def radius=(value)
    @radius = value
    recalc
  end
end
`)

func TestRubySpecSetterMethod(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("circle.rb", rubySetterSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["rb:circle.radius"]; !ok {
		t.Errorf("rb:circle.radius (getter) missing, got %+v", pr.Nodes)
	}
	n, ok := byID["rb:circle.radius="]
	if !ok {
		t.Fatalf("rb:circle.radius= (setter) missing — minted under the wrong name or collided with the getter, got %+v", pr.Nodes)
	}
	if n.Line != 6 || n.EndLine != 9 {
		t.Errorf("radius= Line/EndLine = %d/%d, want 6/9", n.Line, n.EndLine)
	}
}

// TestRubySpecModifierIfDoesNotFalseOpen is this task's brief's explicitly
// called-out trap: a postfix/modifier `if` (`return x if x > 0`, no
// matching "end" anywhere) must NOT be treated as a block opener — if it
// were, KeywordSpan's depth count would go negative relative to reality and
// either close the enclosing def too early or (as here) run off the end of
// the source with ok=false, collapsing EndLine back to Line. The def's real
// body (3 lines: the modifier-if, a plain statement, and the final "end")
// must span correctly, proving the modifier form is invisible to Open.
func TestRubySpecModifierIfDoesNotFalseOpen(t *testing.T) {
	p := SpecParser{S: rubySpec}
	pr, err := p.Parse("guard.rb", []byte(`def clamp(x)
  return 0 if x < 0
  x
end
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	n, ok := byID["rb:guard.clamp"]
	if !ok {
		t.Fatalf("rb:guard.clamp missing, got %+v", pr.Nodes)
	}
	if n.Line != 1 || n.EndLine != 4 {
		t.Errorf("clamp Line/EndLine = %d/%d, want 1/4 (modifier-if must not falsely open a block)", n.Line, n.EndLine)
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
