package index

import (
	"crypto/sha256"
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
)

// goparserSrc is line-sensitive: the Line/EndLine assertions below refer to
// the 1-based line numbers of this literal. Keep them in sync when editing.
var goparserSrc = []byte(`package demo

import "C"

var Answer = 42

type Widget struct {
	N int
}

func (w *Widget) Grow(by int) {
	helper(by)
}

func Saxpy(n int, a float32, x, y []float32) error {
	helper(n)
	other.Do(n)
	C.launch_foo(C.int(n))
	return nil
}

func helper(n int) {}
`)

func parseDemo(t *testing.T) ParseResult {
	t.Helper()
	pr, err := (GoParser{}).Parse("demo/demo.go", goparserSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return pr
}

func nodesByID(pr ParseResult) map[graph.NodeID]graph.Node {
	m := map[graph.NodeID]graph.Node{}
	for _, n := range pr.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestGoParserNodes(t *testing.T) {
	pr := parseDemo(t)
	byID := nodesByID(pr)
	if len(byID) != len(pr.Nodes) {
		t.Fatalf("duplicate node IDs in single-file parse: %+v", pr.Nodes)
	}

	want := map[graph.NodeID]graph.NodeKind{
		"go:demo.Answer":      graph.KVar,
		"go:demo.Widget":      graph.KType,
		"go:demo.Widget.Grow": graph.KMethod,
		"go:demo.Saxpy":       graph.KFunc,
		"go:demo.helper":      graph.KFunc,
	}
	if len(pr.Nodes) != len(want) {
		t.Errorf("got %d nodes, want %d: %+v", len(pr.Nodes), len(want), pr.Nodes)
	}
	for id, kind := range want {
		n, ok := byID[id]
		if !ok {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Kind != kind {
			t.Errorf("%s: Kind = %v, want %v", id, n.Kind, kind)
		}
		if n.Lang != graph.LangGo {
			t.Errorf("%s: Lang = %q, want %q", id, n.Lang, graph.LangGo)
		}
		if n.File != "demo/demo.go" {
			t.Errorf("%s: File = %q, want demo/demo.go", id, n.File)
		}
	}
}

func TestGoParserLines(t *testing.T) {
	byID := nodesByID(parseDemo(t))
	wantLines := map[graph.NodeID][2]int{ // ID -> {Line, EndLine}, 1-based
		"go:demo.Answer":      {5, 5},
		"go:demo.Widget":      {7, 9},
		"go:demo.Widget.Grow": {11, 13},
		"go:demo.Saxpy":       {15, 20},
		"go:demo.helper":      {22, 22},
	}
	for id, ln := range wantLines {
		n, ok := byID[id]
		if !ok {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Line != ln[0] || n.EndLine != ln[1] {
			t.Errorf("%s: Line/EndLine = %d/%d, want %d/%d", id, n.Line, n.EndLine, ln[0], ln[1])
		}
	}
	// a multi-line function must span more than its first line.
	if n := byID["go:demo.Saxpy"]; n.EndLine <= n.Line {
		t.Errorf("Saxpy: EndLine %d not > Line %d", n.EndLine, n.Line)
	}
}

func TestGoParserSig(t *testing.T) {
	byID := nodesByID(parseDemo(t))
	if got, want := byID["go:demo.Saxpy"].Sig, "(n int,a float32,x,y []float32)error"; got != want {
		t.Errorf("Saxpy Sig = %q, want %q", got, want)
	}
	if got, want := byID["go:demo.Widget.Grow"].Sig, "(by int)"; got != want {
		t.Errorf("Grow Sig = %q, want %q", got, want)
	}
	if got, want := byID["go:demo.helper"].Sig, "(n int)"; got != want {
		t.Errorf("helper Sig = %q, want %q", got, want)
	}
}

func TestGoParserCallEdges(t *testing.T) {
	pr := parseDemo(t)

	bySrc := map[graph.NodeID][]graph.Edge{}
	for _, e := range pr.Edges {
		if e.Kind != graph.ECall {
			t.Errorf("unexpected edge kind %v: %+v", e.Kind, e)
		}
		bySrc[e.Src] = append(bySrc[e.Src], e)
	}

	// Grow: one unqualified same-package call.
	growDsts := edgeDsts(bySrc["go:demo.Widget.Grow"])
	if !reflect.DeepEqual(growDsts, map[graph.NodeID]bool{"go:demo.helper": true}) {
		t.Errorf("Grow edges = %v, want only go:demo.helper", growDsts)
	}

	// Saxpy: unqualified helper -> go:demo.helper, qualified other.Do ->
	// go:other.Do; the C.launch_foo / C.int calls must NOT appear (the cgo
	// boundary belongs to resolve.CgoResolver, not the Go parser).
	saxpyDsts := edgeDsts(bySrc["go:demo.Saxpy"])
	wantSaxpy := map[graph.NodeID]bool{"go:demo.helper": true, "go:other.Do": true}
	if !reflect.DeepEqual(saxpyDsts, wantSaxpy) {
		t.Errorf("Saxpy edges = %v, want %v", saxpyDsts, wantSaxpy)
	}
	for dst := range saxpyDsts {
		if dst == "go:C.launch_foo" || dst == "go:C.int" {
			t.Errorf("C.<ident> call leaked into Go call edges: %s", dst)
		}
	}

	// edge sites: helper(n) inside Saxpy is on line 16 of the source literal.
	for _, e := range bySrc["go:demo.Saxpy"] {
		if e.Dst == "go:demo.helper" && e.Line != 16 {
			t.Errorf("Saxpy->helper edge Line = %d, want 16", e.Line)
		}
	}
}

func edgeDsts(es []graph.Edge) map[graph.NodeID]bool {
	m := map[graph.NodeID]bool{}
	for _, e := range es {
		m[e.Dst] = true
	}
	return m
}

// SPX-GRA-001: identical bytes yield byte-identical parse results.
func TestGoParserDeterministic(t *testing.T) {
	a := parseDemo(t)
	b := parseDemo(t)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("parse not deterministic:\nfirst:  %+v\nsecond: %+v", a, b)
	}
	if want := sha256.Sum256(goparserSrc); a.Hash != want {
		t.Errorf("Hash = %x, want sha256 of source %x", a.Hash, want)
	}
}

func TestGoParserSyntaxError(t *testing.T) {
	if _, err := (GoParser{}).Parse("bad.go", []byte("package x\nfunc {")); err == nil {
		t.Error("Parse of invalid source: got nil error, want non-nil")
	}
}
