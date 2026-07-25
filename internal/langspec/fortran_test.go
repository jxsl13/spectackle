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

// fortranGapFixtureSrc reuses gap-fortran/physics.f90's derived-type,
// program, interface, multi-line-continuation-signature, and call-edge
// constructs from R-0005's empirical findings — every one of these was a
// [high] (interface: [medium]) miss before T-0119:
//   - `type :: particle ... end type particle`: no Def pattern covered
//     derived types at all.
//   - `program main ... end program main`: no Def pattern covered the
//     program unit at all.
//   - `interface norm ... end interface norm`: no Def pattern covered
//     named interface blocks at all.
//   - `function total_energy(mass, velocity, &` / next line
//     `height) result(te)`: the old Sig group was mandatory, so a
//     continuation-wrapped argument list made the WHOLE Def fail to match
//     — the function was entirely invisible, not just missing its Sig.
//   - `step` calling `total_energy`/`reset`, `program main` calling
//     `total_energy`/`step`: zero call edges were ever produced for
//     Fortran (CallRe was nil).
var fortranGapFixtureSrc = []byte(`module physics
  implicit none

  type :: particle
     real(kind=8) :: mass
  end type particle

  interface norm
     module procedure norm_real
  end interface norm

contains

  real(kind=8) function kinetic_energy(mass, velocity) result(ke)
    real(kind=8), intent(in) :: mass, velocity
    ke = 0.5d0 * mass * velocity**2
  end function kinetic_energy

  real(kind=8) function total_energy(mass, velocity, &
                                       height) result(te)
    real(kind=8), intent(in) :: mass, velocity, height
    real(kind=8) :: ke, pe
    ke = kinetic_energy(mass, velocity)
    pe = potential_energy(mass, height)
    te = ke + pe
  end function total_energy

  real(kind=8) function potential_energy(mass, height)
    real(kind=8), intent(in) :: mass, height
    potential_energy = mass * 9.81d0 * height
  end function potential_energy

  subroutine step(dt, mass, velocity)
    real(kind=8), intent(in) :: dt
    real(kind=8), intent(inout) :: mass, velocity
    real(kind=8) :: e
    e = total_energy(mass, velocity, 0.0d0)
    call reset(mass)
  end subroutine step

  pure subroutine reset(mass)
    real(kind=8), intent(inout) :: mass
    mass = 0.0d0
  end subroutine reset

  real(kind=8) function norm_real(x) result(n)
    real(kind=8), intent(in) :: x
    n = abs(x)
  end function norm_real

end module physics

program main
  use physics
  implicit none
  real(kind=8) :: e
  type(particle) :: p

  p%mass = 1.0d0
  e = total_energy(p%mass, 2.0d0, 10.0d0)
  call step(0.1d0, p%mass, 2.0d0)
  print *, e
end program main
`)

func TestFortranSpecGapFixtureNewDefsAndEdges(t *testing.T) {
	p := SpecParser{S: fortranSpec}
	pr, err := p.Parse("physics.f90", fortranGapFixtureSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := nodesByID(pr)

	// Derived type: minted as KType, distinct from the bare `module` form.
	if n, ok := byID["f90:physics.particle"]; !ok || n.Kind != graph.KType || n.Line != 4 {
		t.Errorf("f90:physics.particle = %+v ok=%v, want KType at line 4", n, ok)
	}
	// `type(particle) :: p` (a variable declaration, not a definition) must
	// never mint a second "particle" type node or a "type" node.
	if _, ok := byID["f90:physics.type"]; ok {
		t.Error("variable declaration 'type(particle) :: p' must not mint a node named \"type\"")
	}

	// Named interface block: minted as KType.
	if n, ok := byID["f90:physics.norm"]; !ok || n.Kind != graph.KType || n.Line != 8 {
		t.Errorf("f90:physics.norm = %+v ok=%v, want KType at line 8", n, ok)
	}

	// Program unit: minted as KFunc (so its body gets spanned/scanned for
	// calls, like a subroutine's).
	main, ok := byID["f90:physics.main"]
	if !ok || main.Kind != graph.KFunc {
		t.Fatalf("f90:physics.main = %+v ok=%v, want KFunc", main, ok)
	}
	if main.Line != 53 || main.EndLine != 63 {
		t.Errorf("main Line/EndLine = %d/%d, want 53/63", main.Line, main.EndLine)
	}

	// Multi-line continuation signature: the function still mints (name +
	// Line correct) even though its Sig can't be recovered from a single
	// line, and its body still spans correctly across the continuation.
	total, ok := byID["f90:physics.total_energy"]
	if !ok {
		t.Fatalf("f90:physics.total_energy missing (continuation-wrapped signature made the whole Def fail), got %+v", pr.Nodes)
	}
	if total.Line != 19 || total.EndLine != 26 {
		t.Errorf("total_energy Line/EndLine = %d/%d, want 19/26", total.Line, total.EndLine)
	}
	if total.Sig != "" {
		t.Errorf("total_energy Sig = %q, want \"\" (continuation-wrapped signature has no closing ')' on the def line)", total.Sig)
	}
	// A function whose signature DOES close on the def line still gets its
	// Sig, proving the group's new optionality didn't weaken the common case.
	if sig := byID["f90:physics.kinetic_energy"].Sig; sig != "(mass, velocity)" {
		t.Errorf("kinetic_energy Sig = %q, want (mass, velocity)", sig)
	}

	// Call edges: step -> total_energy, step -> reset, main -> total_energy,
	// main -> step. type-spec/attribute keywords adjacent to '(' in the
	// declaration lines (real(, intent(, type() must NOT become edges.
	wantEdges := map[graph.NodeID][]graph.NodeID{
		"f90:physics.step": {"f90:physics.total_energy", "f90:physics.reset"},
		"f90:physics.main": {"f90:physics.total_energy", "f90:physics.step"},
	}
	for src, dsts := range wantEdges {
		for _, dst := range dsts {
			found := false
			for _, e := range pr.Edges {
				if e.Src == src && e.Dst == dst {
					found = true
				}
			}
			if !found {
				t.Errorf("missing ECall edge %s -> %s, got edges %+v", src, dst, pr.Edges)
			}
		}
	}
	for _, e := range pr.Edges {
		if e.Dst == "f90:physics.real" || e.Dst == "f90:physics.intent" || e.Dst == "f90:physics.type" {
			t.Errorf("got a phantom edge to a type-spec/attribute keyword: %+v", e)
		}
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
