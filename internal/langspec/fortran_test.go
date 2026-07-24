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

// fortranSrc exercises all three fortranSpec Defs (typed/attributed
// function, subroutine with and without parens, bare module) plus the
// negative lines that must NOT mint nodes: every `end ...` form (the
// end-keyword trap the cookbook documents), `module procedure`, a call
// site, and a comment.
var fortranSrc = []byte(`module linalg

pure real(kind=8) function det(m) result(d)
end function det

RECURSIVE FUNCTION fib(n) RESULT(r)
end function fib

subroutine step(dt)
  call reset
end subroutine step

pure subroutine reset
end subroutine reset

module procedure solve
! subroutine commented(x)
end module linalg
`)

func TestFortranSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: fortranSpec}
	if p.Lang() != graph.LangF90 {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangF90)
	}
	if got, want := p.Extensions(), []string{".f90", ".f95", ".f03", ".f08"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestFortranSpecNodes(t *testing.T) {
	p := SpecParser{S: fortranSpec}
	pr, err := p.Parse("num/solver.f90", fortranSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"f90:solver.linalg": {graph.KType, 1},
		"f90:solver.det":    {graph.KFunc, 3},
		"f90:solver.fib":    {graph.KFunc, 6},
		"f90:solver.step":   {graph.KFunc, 9},
		"f90:solver.reset":  {graph.KFunc, 13},
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
		if n.Lang != graph.LangF90 {
			t.Errorf("%s Lang = %v, want f90", id, n.Lang)
		}
	}
	if sig := byID["f90:solver.det"].Sig; sig != "(m)" {
		t.Errorf("det Sig = %q, want (m)", sig)
	}
}

// TestFortranSpecEndLinesNeverMint pins the end-keyword trap: every
// `end function/subroutine/module` line, `module procedure`, a call site
// and a comment mint nothing.
func TestFortranSpecEndLinesNeverMint(t *testing.T) {
	p := SpecParser{S: fortranSpec}
	pr, err := p.Parse("neg.f90", []byte(`end function det
END SUBROUTINE step
end module linalg
module procedure solve
  call reset
! subroutine commented(x)
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Nodes) != 0 {
		t.Fatalf("got %d nodes, want 0: %+v", len(pr.Nodes), pr.Nodes)
	}
}

func TestFortranSpecRegisteredInAll(t *testing.T) {
	found := false
	for _, p := range All() {
		if p.Lang() == graph.LangF90 {
			found = true
		}
	}
	if !found {
		t.Error("All() does not contain a parser for graph.LangF90 — fortranSpec not registered via init()")
	}
}

func TestFortranSpecDeterministic(t *testing.T) {
	p := SpecParser{S: fortranSpec}
	pr1, err := p.Parse("num/solver.f90", fortranSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("num/solver.f90", fortranSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestFortranSpecIndexAllE2E proves the full pipeline: a real .f90 file on
// disk through index.New + IndexAll mints the f90:<stem>.<name> nodes.
func TestFortranSpecIndexAllE2E(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "solver.f90"), fortranSrc, 0o644); err != nil {
		t.Fatal(err)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	for id, kind := range map[graph.NodeID]graph.NodeKind{
		"f90:solver.det":    graph.KFunc,
		"f90:solver.step":   graph.KFunc,
		"f90:solver.linalg": graph.KType,
	} {
		n, ok := g.Node(id)
		if !ok {
			t.Fatalf("node %s missing after IndexAll", id)
		}
		if n.Kind != kind || n.Lang != graph.LangF90 {
			t.Errorf("%s = %+v, want Kind=%v Lang=f90", id, n, kind)
		}
	}
}
