package langspec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jxsl13/spectacle/internal/graph"
	"github.com/jxsl13/spectacle/internal/index"
	"github.com/jxsl13/spectacle/internal/resolve"
	"github.com/jxsl13/spectacle/internal/store"
)

// cSrc exercises every cSpec Def shape plus every false-positive trap called
// out in the task brief: a prototype, a static definition, a function-like
// macro, a struct/typedef/enum, a #include (must not match, no leading
// [A-Za-z_]), and indented control-flow/return/call statements (must not
// match, no leading whitespace allowed by the anchor).
var cSrc = []byte(`#include <stddef.h>

/* prototype */
int launch_saxpy(int n, float a, const float *x, float *y);

struct Point {
    int x, y;
};

typedef struct Named {
    int id;
} Named;

enum Color { RED, GREEN, BLUE };

#define MAX(a, b) ((a) > (b) ? (a) : (b))

static void helper(void) {
    if (helper_called) {
        return;
    }
    for (int i = 0; i < 10; i++) {
        while (i > 0) {
            switch (i) {
            case 1:
                return;
            }
        }
    }
    do_something(1, 2);
}
`)

func cNodesByID(pr index.ParseResult) map[graph.NodeID]graph.Node {
	m := map[graph.NodeID]graph.Node{}
	for _, n := range pr.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestCSpecLangExtensions(t *testing.T) {
	p := SpecParser{S: cSpec}
	if p.Lang() != graph.LangC {
		t.Errorf("Lang() = %v, want %v", p.Lang(), graph.LangC)
	}
	if got, want := p.Extensions(), []string{".c", ".h"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Extensions() = %v, want %v", got, want)
	}
}

func TestCSpecNodes(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byID := cNodesByID(pr)

	want := map[graph.NodeID]struct {
		Kind graph.NodeKind
		Line int
	}{
		"c:launch_saxpy": {graph.KFunc, 4},
		"c:Point":        {graph.KType, 6},
		"c:Named":        {graph.KType, 10},
		"c:Color":        {graph.KType, 14},
		"c:MAX":          {graph.KFunc, 16},
		"c:helper":       {graph.KFunc, 18},
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
		if n.Lang != graph.LangC {
			t.Errorf("%s Lang = %v, want c", id, n.Lang)
		}
	}
}

// TestCSpecNoFalsePositivesOnControlFlow is the acceptance-brief guard: a
// fixture full of indented if/for/while/switch/return/call statements must
// never mint a "c:if", "c:for", "c:while", "c:switch", "c:return" or
// "c:do_something" node. It also checks #include never mints a node (its
// line starts with '#', which the function Def's anchor structurally
// excludes since it requires [A-Za-z_] as the first character).
func TestCSpecNoFalsePositivesOnControlFlow(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	banned := []graph.NodeID{"c:if", "c:for", "c:while", "c:switch", "c:return", "c:sizeof", "c:do_something", "c:helper_called", "c:stddef"}
	byID := cNodesByID(pr)
	for _, id := range banned {
		if n, ok := byID[id]; ok {
			t.Errorf("false positive node %s = %+v, must not be minted from control-flow/call/include lines", id, n)
		}
	}
}

func TestCSpecNoEdges(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr, err := p.Parse("x.c", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pr.Edges) != 0 {
		t.Errorf("cSpec Parse must not emit edges, got %+v", pr.Edges)
	}
}

func TestCSpecDeterministic(t *testing.T) {
	p := SpecParser{S: cSpec}
	pr1, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pr2, err := p.Parse("kernels/saxpy.h", cSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(pr1, pr2) {
		t.Errorf("Parse is not deterministic across identical runs:\n%+v\nvs\n%+v", pr1, pr2)
	}
}

// TestIndexAllSaxpyExampleCChainWithHeader is the e2e acceptance test named
// in the task brief: index the real examples/saxpy fixture with GoParser +
// CudaParser + langspec.All() (which now includes cSpec) and confirm the
// full go -> c -> cu chain holds, with c:launch_saxpy a REAL, located node
// (File != "" and Line > 0) rather than a cgo-resolver stub. Both
// saxpy/kernels/saxpy.h (our new C parser) and saxpy/kernels/saxpy.cu
// (index.CudaParser's extern "C" wrapper) define launch_saxpy; per
// internal/index.disambiguate, whichever file sorts first claims the base
// "c:launch_saxpy" ID deterministically ("saxpy.cu" < "saxpy.h"
// lexicographically), so this test intentionally does not pin down which of
// the two files wins — only that the node is real.
func TestIndexAllSaxpyExampleCChainWithHeader(t *testing.T) {
	root, err := filepath.Abs(filepath.FromSlash("../../examples/saxpy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(root); statErr != nil {
		t.Skipf("examples/saxpy not available: %v", statErr)
	}

	g := graph.NewMem()
	parsers := append([]index.LanguageParser{index.GoParser{}, index.CudaParser{}}, All()...)
	idx := index.New(g, store.NewMem(), parsers, resolve.Default().All())
	if _, err := idx.IndexAll(context.Background(), root); err != nil {
		t.Fatalf("IndexAll(%s): %v", root, err)
	}

	// (a) c:launch_saxpy is a real, located node — not a stub — regardless
	// of which of saxpy.h/saxpy.cu won the disambiguation race.
	wrapper, ok := g.Node("c:launch_saxpy")
	if !ok {
		t.Fatal("node c:launch_saxpy missing")
	}
	if wrapper.File == "" {
		t.Error("c:launch_saxpy File is empty, want a real source location")
	}
	if wrapper.Line <= 0 {
		t.Errorf("c:launch_saxpy Line = %d, want > 0 (real node, not stub)", wrapper.Line)
	}
	if wrapper.Kind != graph.KFunc {
		t.Errorf("c:launch_saxpy Kind = %v, want KFunc", wrapper.Kind)
	}

	// (b) go:saxpy.Saxpy -ECgo-> c:launch_saxpy
	cgo := g.Neighbors("go:saxpy.Saxpy", graph.Out, []graph.EdgeKind{graph.ECgo})
	foundCgo := false
	for _, e := range cgo {
		if e.Dst == "c:launch_saxpy" {
			foundCgo = true
		}
	}
	if !foundCgo {
		t.Errorf("Neighbors(go:saxpy.Saxpy, Out, ECgo) = %+v, want edge to c:launch_saxpy", cgo)
	}

	// (c) c:launch_saxpy -ELaunch-> cu:saxpy_kernel
	launch := g.Neighbors("c:launch_saxpy", graph.Out, []graph.EdgeKind{graph.ELaunch})
	foundLaunch := false
	for _, e := range launch {
		if e.Dst == "cu:saxpy_kernel" {
			foundLaunch = true
		}
	}
	if !foundLaunch {
		t.Errorf("Neighbors(c:launch_saxpy, Out, ELaunch) = %+v, want edge to cu:saxpy_kernel", launch)
	}

	kernel, ok := g.Node("cu:saxpy_kernel")
	if !ok {
		t.Fatal("node cu:saxpy_kernel missing")
	}
	if kernel.Kind != graph.KKernel || kernel.Lang != graph.LangCuda {
		t.Errorf("cu:saxpy_kernel = %+v, want Kind=KKernel Lang=cu", kernel)
	}
}
