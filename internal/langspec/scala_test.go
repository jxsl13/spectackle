package langspec

import (
	"reflect"
	"testing"

	"github.com/jxsl13/spectackle/internal/graph"
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
		Kind    graph.NodeKind
		Line    int
		EndLine int // 0 means "same as Line"
	}{
		"scala:App.Point":  {graph.KType, 1, 0}, // case class
		"scala:App.Foo":    {graph.KType, 3, 0}, // object
		"scala:App.run":    {graph.KFunc, 4, 9}, // plain def; brace-counted body now that CallRe is wired (R-0005)
		"scala:App.helper": {graph.KFunc, 11, 13},
		"scala:App.Shape":  {graph.KType, 16, 0}, // trait
		"scala:App.area":   {graph.KFunc, 17, 0}, // paramless def, no body: span unchanged
		"scala:App.Empty":  {graph.KType, 20, 0}, // case object
	}
	if len(pr.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, w := range want {
		n, ok := byID[id]
		if !ok {
			t.Fatalf("node %s missing, got %+v", id, pr.Nodes)
		}
		wantEnd := w.EndLine
		if wantEnd == 0 {
			wantEnd = w.Line
		}
		if n.Kind != w.Kind {
			t.Errorf("%s Kind = %v, want %v", id, n.Kind, w.Kind)
		}
		if n.Line != w.Line || n.EndLine != wantEnd {
			t.Errorf("%s Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, w.Line, wantEnd)
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

// scalaGapSrc reproduces the R-0005 gap-fixture constructs previously
// missed by scalaSpec (findings/scala.md, scratch fixture
// gap-scala/Sample.scala): a Scala 3 single-line extension method, a
// given/typeclass instance, a val bound to a lambda, a captured Sig, and
// the intra-file calls that exercise the CallRe wiring fixing both call
// edges and EndLine spans.
var scalaGapSrc = []byte(`extension (x: Int) def triple: Int = x * 3

given intOrdering: Ordering[Int] with
  def compare(x: Int, y: Int): Int = x - y

val square = (x: Int) => x * x

def caller(x: Int): Int = {
  square(x) + compare(1, 2)
}
`)

// TestScalaSpecGapFixes pins down the R-0005 [high]/[medium] fixes: the
// single-line extension-method Def, the given-instance Def, the
// val-bound-lambda Def, Def.Sig capture, and CallRe-driven edges/EndLine
// spans.
func TestScalaSpecGapFixes(t *testing.T) {
	p := SpecParser{S: scalaSpec}
	pr, err := p.Parse("pkg/gap.scala", scalaGapSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind    graph.NodeKind
		Line    int
		EndLine int
		Sig     string
	}{
		"scala:gap.triple":      {graph.KFunc, 1, 1, ""},               // Scala 3 single-line extension method
		"scala:gap.intOrdering": {graph.KType, 3, 3, ""},               // given/typeclass instance
		"scala:gap.compare":     {graph.KFunc, 4, 4, "x: Int, y: Int"}, // nested def inside the given, plus Sig capture
		"scala:gap.square":      {graph.KFunc, 6, 6, ""},               // val bound to a lambda
		"scala:gap.caller":      {graph.KFunc, 8, 10, "x: Int"},        // calls square(x), compare(1, 2)
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
		if n.Sig != w.Sig {
			t.Errorf("%s Sig = %q, want %q", id, n.Sig, w.Sig)
		}
	}

	wantEdges := map[[2]graph.NodeID]bool{
		{"scala:gap.caller", "scala:gap.square"}:  false,
		{"scala:gap.caller", "scala:gap.compare"}: false,
	}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("edge Kind = %v, want ECall", e.Kind)
		}
		key := [2]graph.NodeID{e.Src, e.Dst}
		if _, ok := wantEdges[key]; ok {
			wantEdges[key] = true
		}
	}
	for k, seen := range wantEdges {
		if !seen {
			t.Errorf("missing call edge %s -> %s, got edges %+v", k[0], k[1], pr.Edges)
		}
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
