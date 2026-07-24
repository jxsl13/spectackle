package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// scalaSrc exercises every scalaSpec Def (case class, object, def, trait,
// case object) plus negative lines that must NOT mint nodes: an if line and
// a plain call line inside a method body.
var scalaSrc = []byte(`case class Point(x: Int, y: Int)

object Foo {
  def run(x: Int): Int = {
    if (x > 0) {
      println(x)
    }
    x
  }

  private def helper(): Unit = {
    helper()
  }
}

trait Shape {
  def area: Double
}

case object Empty
`)

func TestScalaSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: scalaSpec}
	if p.Lang() != graph.Lang("scala") {
		t.Errorf("Lang() = %v, want scala", p.Lang())
	}
	if got, want := p.Extensions(), []string{".scala"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestScalaSpecNodes(t *testing.T) {
	p := SpecParser{S: scalaSpec}
	pr, err := p.Parse("pkg/App.scala", scalaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"scala:App.Point":  {graph.KType, 1}, // case class
		"scala:App.Foo":    {graph.KType, 3}, // object
		"scala:App.run":    {graph.KFunc, 4}, // plain def
		"scala:App.helper": {graph.KFunc, 11},
		"scala:App.Shape":  {graph.KType, 16}, // trait
		"scala:App.area":   {graph.KFunc, 17}, // paramless def
		"scala:App.Empty":  {graph.KType, 20}, // case object
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
		if n.Lang != graph.Lang("scala") {
			t.Errorf("%s Lang = %v, want scala", id, n.Lang)
		}
		if n.File != "pkg/App.scala" {
			t.Errorf("%s File = %q, want pkg/App.scala", id, n.File)
		}
	}
}

// TestScalaSpecNegativeLines pins down that a `val` decl, an if-statement,
// and a bare call expression never mint nodes, while a plain `def` (no
// leading modifier required, unlike java/cs) still does.
func TestScalaSpecNegativeLines(t *testing.T) {
	p := SpecParser{S: scalaSpec}
	pr, err := p.Parse("Neg.scala", []byte(`class Neg {
  val x: Int = 1
  def run(): Unit = {
    if (x > 0) {
      println(x)
    }
  }
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)
	if _, ok := byID["scala:Neg.x"]; ok {
		t.Error("val decl `val x: Int = 1` must not mint a node")
	}
	if len(pr.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (class Neg + def run): %+v", len(pr.Nodes), pr.Nodes)
	}
	if _, ok := byID["scala:Neg.Neg"]; !ok {
		t.Errorf("class Neg missing, got %+v", pr.Nodes)
	}
	if _, ok := byID["scala:Neg.run"]; !ok {
		t.Errorf("def run missing, got %+v", pr.Nodes)
	}
}

func TestScalaSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.Lang("scala") {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.Lang(\"scala\") — scalaSpec not registered via init()")
	}
}

func TestScalaSpecDeterministic(t *testing.T) {
	p := SpecParser{S: scalaSpec}
	pr1, err := p.Parse("pkg/App.scala", scalaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("pkg/App.scala", scalaSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}
